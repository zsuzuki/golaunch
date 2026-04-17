package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Arg struct {
	Value   string `toml:"value"`
	Enabled bool   `toml:"enabled"`
}

type CommandDef struct {
	Title   string `toml:"title"`
	Command string `toml:"command"`
	Args    []Arg  `toml:"args"`
}

type Config struct {
	OutputBufferBytes int          `toml:"output_buffer_bytes"`
	Commands          []CommandDef `toml:"commands"`
}

type screen int

const (
	screenList screen = iota
	screenEdit
)

type inputTarget int

const (
	inputTitle inputTarget = iota
	inputCommand
	inputArg
)

type runResultMsg struct {
	stdout string
	stderr string
	err    error
}

type processStartedMsg struct {
	proc *runningProcess
}

type runningProcess struct {
	cmd  *exec.Cmd
	done chan runResultMsg
}

type model struct {
	cwd string

	cfg        Config
	configPath string

	width  int
	height int

	screen screen

	listCursor    int
	confirmDelete bool
	confirmQuit   bool

	editIndex  int
	editCursor int

	inputActive bool
	inputTarget inputTarget
	inputArgIdx int
	input       textinput.Model

	running  bool
	output   string
	lastCmd  string
	proc     *runningProcess
	outputVP viewport.Model

	styles styles
}

const defaultOutputBufferBytes = 1024 * 1024

type styles struct {
	base     lipgloss.Style
	pane     lipgloss.Style
	head     lipgloss.Style
	normal   lipgloss.Style
	selected lipgloss.Style
	hint     lipgloss.Style
	danger   lipgloss.Style
	outTitle lipgloss.Style
	outBody  lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		base:   lipgloss.NewStyle().Padding(1, 1),
		pane:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(1, 1),
		head:   lipgloss.NewStyle().Bold(true),
		normal: lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		selected: lipgloss.NewStyle().
			Background(lipgloss.Color("153")).
			Foreground(lipgloss.Color("236")).
			Bold(true),
		hint:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		danger:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
		outTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		outBody:  lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	}
}

func newModel() model {
	cwd, _ := os.Getwd()
	cfgPath, err := resolveConfigPath()
	if err != nil {
		cfgPath = "./golaunch.toml"
	}

	cfg, _ := loadConfig(cfgPath)

	ti := textinput.New()
	ti.Placeholder = ""
	ti.Focus()

	ti.CharLimit = 512

	ti.Width = 40

	vp := viewport.New(1, 1)

	m := model{
		cwd:        cwd,
		cfg:        cfg,
		configPath: cfgPath,
		screen:     screenList,
		styles:     defaultStyles(),
		input:      ti,
		lastCmd:    "(none)",
		outputVP:   vp,
	}
	m.setOutput("Output will appear here.\n")
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width/3)
		m.syncOutputViewport(false)
		return m, nil

	case runResultMsg:
		m.running = false
		m.proc = nil
		var b strings.Builder
		if msg.err != nil {
			b.WriteString("Execution error: ")
			b.WriteString(msg.err.Error())
			b.WriteString("\n\n")
		}
		if msg.stdout != "" {
			b.WriteString("[stdout]\n")
			b.WriteString(msg.stdout)
			if !strings.HasSuffix(msg.stdout, "\n") {
				b.WriteString("\n")
			}
		}
		if msg.stderr != "" {
			if msg.stdout != "" {
				b.WriteString("\n")
			}
			b.WriteString("[stderr]\n")
			b.WriteString(msg.stderr)
			if !strings.HasSuffix(msg.stderr, "\n") {
				b.WriteString("\n")
			}
		}
		if b.Len() == 0 {
			b.WriteString("(no output)\n")
		}
		m.setOutput(b.String())
		return m, nil

	case processStartedMsg:
		m.proc = msg.proc
		if m.proc == nil {
			m.running = false
			return m, nil
		}
		return m, waitForProcessDone(m.proc.done)
	}

	if m.inputActive {
		return m.updateInput(msg)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if m.handleOutputScroll(keyMsg) {
			return m, nil
		}

		if m.running {
			return m.handleRunningKey(keyMsg)
		}

		if m.confirmQuit {
			switch keyMsg.String() {
			case "y":
				return m, tea.Quit
			case "n", "esc":
				m.confirmQuit = false
				return m, nil
			default:
				return m, nil
			}
		}

		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			m.confirmQuit = true
			return m, nil
		}
	}

	switch m.screen {
	case screenList:
		return m.updateList(msg)
	case screenEdit:
		return m.updateEdit(msg)
	default:
		return m, nil
	}
}

func (m model) handleRunningKey(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyMsg.String() {
	case "ctrl+c", "i":
		return m.sendSignal(syscall.SIGINT)
	case "ctrl+\\\\", "K":
		return m.sendSignal(syscall.SIGKILL)
	default:
		return m, nil
	}
}

func (m *model) handleOutputScroll(keyMsg tea.KeyMsg) bool {
	switch keyMsg.String() {
	case "pgup":
		m.outputVP.ViewUp()
		return true
	case "pgdown":
		m.outputVP.ViewDown()
		return true
	case "home":
		m.outputVP.GotoTop()
		return true
	case "end":
		m.outputVP.GotoBottom()
		return true
	default:
		return false
	}
}

func (m model) sendSignal(sig syscall.Signal) (tea.Model, tea.Cmd) {
	if m.proc == nil || m.proc.cmd == nil || m.proc.cmd.Process == nil {
		m.appendOutput(fmt.Sprintf("Cannot send %s: no active process.\n", signalName(sig)))
		return m, nil
	}

	pid := m.proc.cmd.Process.Pid
	err := syscall.Kill(-pid, sig)
	if err != nil {
		err = m.proc.cmd.Process.Signal(sig)
	}
	if err != nil {
		m.appendOutput(fmt.Sprintf("Failed to send %s: %v\n", signalName(sig), err))
		return m, nil
	}

	m.appendOutput(fmt.Sprintf("Sent %s to running process (pid=%d)\n", signalName(sig), pid))
	return m, nil
}

func (m *model) appendOutput(s string) {
	if m.output == "" {
		m.setOutput(s)
		return
	}
	next := m.output
	if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += s
	m.setOutput(next)
}

func (m *model) setOutput(s string) {
	m.output = limitOutputBytes(s, m.outputBufferBytes())
	m.syncOutputViewport(true)
}

func (m model) outputBufferBytes() int {
	if m.cfg.OutputBufferBytes > 0 {
		return m.cfg.OutputBufferBytes
	}
	return defaultOutputBufferBytes
}

func (m *model) syncOutputViewport(stickToBottom bool) {
	bodyWidth, bodyHeight := m.rightPaneBodySize(m.rightPaneWidth(), m.paneBodyHeight())

	wasAtBottom := m.outputVP.AtBottom()
	m.outputVP.Width = bodyWidth
	m.outputVP.Height = bodyHeight
	m.outputVP.SetContent(wrapOutput(m.output, bodyWidth))

	if stickToBottom || wasAtBottom {
		m.outputVP.GotoBottom()
	}
}

func (m model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.inputActive = false
			return m, nil
		case "enter":
			value := m.input.Value()
			if m.editIndex >= 0 && m.editIndex < len(m.cfg.Commands) {
				switch m.inputTarget {
				case inputTitle:
					m.cfg.Commands[m.editIndex].Title = value
				case inputCommand:
					m.cfg.Commands[m.editIndex].Command = value
				case inputArg:
					if m.inputArgIdx >= 0 && m.inputArgIdx < len(m.cfg.Commands[m.editIndex].Args) {
						m.cfg.Commands[m.editIndex].Args[m.inputArgIdx].Value = value
					}
				}
				_ = saveConfig(m.configPath, m.cfg)
			}
			m.inputActive = false
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.confirmDelete {
		switch keyMsg.String() {
		case "y":
			if m.listCursor >= 0 && m.listCursor < len(m.cfg.Commands) {
				m.cfg.Commands = append(m.cfg.Commands[:m.listCursor], m.cfg.Commands[m.listCursor+1:]...)
				if m.listCursor >= len(m.cfg.Commands) && m.listCursor > 0 {
					m.listCursor--
				}
				_ = saveConfig(m.configPath, m.cfg)
			}
			m.confirmDelete = false
			return m, nil
		case "n", "esc":
			m.confirmDelete = false
			return m, nil
		default:
			return m, nil
		}
	}

	switch keyMsg.String() {
	case "up", "k":
		if m.listCursor > 0 {
			m.listCursor--
		}
	case "down", "j":
		if m.listCursor < len(m.cfg.Commands)-1 {
			m.listCursor++
		}
	case "enter":
		if len(m.cfg.Commands) == 0 {
			return m, nil
		}
		m.screen = screenEdit
		m.editIndex = m.listCursor
		m.editCursor = 0
	case "a":
		m.cfg.Commands = append(m.cfg.Commands, CommandDef{})
		m.listCursor = len(m.cfg.Commands) - 1
		m.editIndex = m.listCursor
		m.editCursor = 0
		m.screen = screenEdit
		_ = saveConfig(m.configPath, m.cfg)
	case "c":
		if len(m.cfg.Commands) == 0 || m.listCursor < 0 || m.listCursor >= len(m.cfg.Commands) {
			return m, nil
		}
		src := m.cfg.Commands[m.listCursor]
		clone := CommandDef{
			Title:   src.Title,
			Command: src.Command,
			Args:    append([]Arg(nil), src.Args...),
		}
		insertAt := m.listCursor + 1
		m.cfg.Commands = append(m.cfg.Commands[:insertAt], append([]CommandDef{clone}, m.cfg.Commands[insertAt:]...)...)
		m.listCursor = insertAt
		_ = saveConfig(m.configPath, m.cfg)
	case "d":
		if len(m.cfg.Commands) > 0 {
			m.confirmDelete = true
		}
	case "r":
		if len(m.cfg.Commands) == 0 {
			return m, nil
		}
		cmdRef := m.cfg.Commands[m.listCursor]
		if strings.TrimSpace(cmdRef.Command) == "" {
			m.setOutput("Execution error: command is empty\n")
			return m, nil
		}
		m.running = true
		m.inputActive = false
		m.confirmDelete = false
		m.confirmQuit = false
		m.lastCmd = buildCommandLine(cmdRef)
		m.setOutput("Running...\n")
		return m, startRunCommand(m.cwd, cmdRef)
	}
	return m, nil
}

func (m model) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.editIndex < 0 || m.editIndex >= len(m.cfg.Commands) {
		m.screen = screenList
		return m, nil
	}

	cmdRef := &m.cfg.Commands[m.editIndex]
	maxCursor := 1 + len(cmdRef.Args)

	switch keyMsg.String() {
	case "esc":
		m.screen = screenList
		return m, nil
	case "up", "k":
		if m.editCursor > 0 {
			m.editCursor--
		}
	case "down", "j":
		if m.editCursor < maxCursor {
			m.editCursor++
		}
	case "+":
		argIdx := m.editCursor - 2
		if argIdx >= 0 && argIdx < len(cmdRef.Args) {
			insertAt := argIdx + 1
			cmdRef.Args = append(cmdRef.Args[:insertAt], append([]Arg{{}}, cmdRef.Args[insertAt:]...)...)
			m.editCursor = 2 + insertAt
		} else {
			cmdRef.Args = append(cmdRef.Args, Arg{})
			m.editCursor = 2 + len(cmdRef.Args) - 1
		}
		_ = saveConfig(m.configPath, m.cfg)
	case "ctrl+d", "del", "delete":
		argIdx := m.editCursor - 2
		if argIdx >= 0 && argIdx < len(cmdRef.Args) {
			cmdRef.Args = append(cmdRef.Args[:argIdx], cmdRef.Args[argIdx+1:]...)
			lastIdx := 1 + len(cmdRef.Args)
			if m.editCursor > lastIdx {
				m.editCursor = lastIdx
			}
			_ = saveConfig(m.configPath, m.cfg)
		}
	case "r":
		_ = saveConfig(m.configPath, m.cfg)
		if cmdRef.Command == "" {
			m.setOutput("Execution error: command is empty\n")
			return m, nil
		}
		m.running = true
		m.inputActive = false
		m.confirmQuit = false
		m.lastCmd = buildCommandLine(*cmdRef)
		m.setOutput("Running...\n")
		return m, startRunCommand(m.cwd, *cmdRef)
	case " ":
		argIdx := m.editCursor - 2
		if argIdx >= 0 && argIdx < len(cmdRef.Args) {
			cmdRef.Args[argIdx].Enabled = !cmdRef.Args[argIdx].Enabled
			_ = saveConfig(m.configPath, m.cfg)
		}
	case "enter":
		switch {
		case m.editCursor == 0:
			m.startInput(inputTitle, -1, cmdRef.Title)
		case m.editCursor == 1:
			m.startInput(inputCommand, -1, cmdRef.Command)
		case m.editCursor >= 2 && m.editCursor < 2+len(cmdRef.Args):
			argIdx := m.editCursor - 2
			m.startInput(inputArg, argIdx, cmdRef.Args[argIdx].Value)
		}
	}

	return m, nil
}

func (m *model) startInput(target inputTarget, argIdx int, current string) {
	m.inputTarget = target
	m.inputArgIdx = argIdx
	m.input.SetValue(current)
	m.input.CursorEnd()
	m.inputActive = true
}

func startRunCommand(cwd string, cmdDef CommandDef) tea.Cmd {
	return func() tea.Msg {
		args := make([]string, 0, len(cmdDef.Args))
		for _, a := range cmdDef.Args {
			if a.Enabled {
				args = append(args, a.Value)
			}
		}

		cmd := exec.Command(cmdDef.Command, args...)
		cmd.Dir = cwd
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		proc := &runningProcess{
			cmd:  cmd,
			done: make(chan runResultMsg, 1),
		}

		if err := cmd.Start(); err != nil {
			proc.done <- runResultMsg{
				stdout: stdout.String(),
				stderr: stderr.String(),
				err:    err,
			}
			close(proc.done)
			return processStartedMsg{proc: proc}
		}

		go func() {
			err := cmd.Wait()
			proc.done <- runResultMsg{
				stdout: stdout.String(),
				stderr: stderr.String(),
				err:    err,
			}
			close(proc.done)
		}()

		return processStartedMsg{proc: proc}
	}
}

func waitForProcessDone(done <-chan runResultMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-done
		if !ok {
			return runResultMsg{err: fmt.Errorf("process ended without result")}
		}
		return msg
	}
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGKILL:
		return "SIGKILL"
	default:
		return fmt.Sprintf("signal %d", sig)
	}
}

func buildCommandLine(cmdDef CommandDef) string {
	args := make([]string, 0, len(cmdDef.Args))
	for _, a := range cmdDef.Args {
		if a.Enabled {
			args = append(args, shellQuote(a.Value))
		}
	}
	cmd := shellQuote(cmdDef.Command)
	if len(args) == 0 {
		return cmd
	}
	return cmd + " " + strings.Join(args, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`!&|;()<>*?[]{}") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	leftW := m.leftPaneWidth()
	rightW := m.rightPaneWidth()
	bodyH := m.paneBodyHeight()

	left := m.renderLeft(leftW, bodyH)
	right := m.renderRight(rightW, bodyH)

	return m.styles.base.Render(lipgloss.JoinHorizontal(lipgloss.Top, left, right))
}

func (m model) leftPaneWidth() int {
	return min(52, max(36, m.width/2))
}

func (m model) rightPaneWidth() int {
	return max(20, m.width-m.leftPaneWidth()-3)
}

func (m model) paneBodyHeight() int {
	return max(8, m.height-4)
}

func (m model) renderLeft(w, h int) string {
	content := ""
	if m.screen == screenList {
		content = m.renderList()
	} else {
		content = m.renderEdit()
	}

	pane := m.styles.pane.Width(w).Height(h)
	return pane.Render(content)
}

func (m model) renderList() string {
	var lines []string
	lines = append(lines, m.styles.head.Render(fmt.Sprintf("Current Dir: %s", m.cwd)))
	lines = append(lines, "")

	if len(m.cfg.Commands) == 0 {
		lines = append(lines, m.styles.hint.Render("(no commands)"))
	} else {
		for i, c := range m.cfg.Commands {
			title := c.Title
			if strings.TrimSpace(title) == "" {
				title = "(untitled command)"
			}
			line := "  " + title
			if i == m.listCursor {
				line = m.styles.selected.Render(line)
			} else {
				line = m.styles.normal.Render(line)
			}
			lines = append(lines, line)
		}
	}

	lines = append(lines, "")
	if m.confirmDelete {
		lines = append(lines, m.styles.danger.Render("Delete selected command? (y/n)"))
	} else if m.running {
		lines = append(lines, m.styles.danger.Render("RUNNING: controls locked  ctrl+c/i: SIGINT  ctrl+\\ or K: SIGKILL"))
	} else if m.confirmQuit {
		lines = append(lines, m.styles.danger.Render("Quit golaunch? (y/n)"))
	} else {
		lines = append(lines, m.styles.hint.Render("up/down: select  enter: edit  r: run  a: add  c: clone  d: delete  q: quit"))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderEdit() string {
	if m.editIndex < 0 || m.editIndex >= len(m.cfg.Commands) {
		return "invalid command index"
	}
	cmdRef := m.cfg.Commands[m.editIndex]

	var lines []string
	lines = append(lines, m.styles.head.Render(fmt.Sprintf("Current Dir: %s", m.cwd)))
	lines = append(lines, "")

	titleLine := "Title: " + cmdRef.Title
	if m.editCursor == 0 {
		titleLine = m.renderSelectedEditLine(titleLine, inputTitle, -1)
	} else {
		titleLine = m.styles.normal.Render(titleLine)
	}
	lines = append(lines, titleLine)

	cmdLine := "Command: " + cmdRef.Command
	if m.editCursor == 1 {
		cmdLine = m.renderSelectedEditLine(cmdLine, inputCommand, -1)
	} else {
		cmdLine = m.styles.normal.Render(cmdLine)
	}
	lines = append(lines, cmdLine)

	lines = append(lines, m.styles.normal.Render("Args:"))
	for i, a := range cmdRef.Args {
		mark := " "
		if a.Enabled {
			mark = "*"
		}
		argLine := fmt.Sprintf("      [%s]: %s", mark, a.Value)
		if m.editCursor == i+2 {
			argLine = m.renderSelectedEditLine(argLine, inputArg, i)
		} else {
			argLine = m.styles.normal.Render(argLine)
		}
		lines = append(lines, argLine)
	}

	lines = append(lines, "")
	if m.running {
		lines = append(lines, m.styles.danger.Render("RUNNING: controls locked  ctrl+c/i: SIGINT  ctrl+\\ or K: SIGKILL"))
	} else {
		lines = append(lines, m.styles.hint.Render("up/down: move  enter: edit  r: run  +: add below  space: toggle arg  ctrl+d/del: delete arg  esc: back  q: quit"))
	}
	if m.confirmQuit {
		lines = append(lines, m.styles.danger.Render("Quit golaunch? (y/n)"))
	}
	if m.inputActive {
		lines = append(lines, m.styles.hint.Render("Editing: type and press Enter to save (Esc to cancel)"))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderSelectedEditLine(fallback string, target inputTarget, argIdx int) string {
	if m.inputActive && m.inputTarget == target && m.inputArgIdx == argIdx {
		return m.styles.selected.Render("  " + m.input.View())
	}
	return m.styles.selected.Render(fallback)
}

func (m model) renderRight(w, h int) string {
	bodyWidth, bodyHeight := m.rightPaneBodySize(w, h)
	cmdLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Width(bodyWidth).
		MaxWidth(bodyWidth).
		Render("Command: " + m.lastCmd)
	title := m.styles.outTitle.Render("I/O")
	body := m.styles.outBody.Render(m.outputVP.View())
	pane := m.styles.pane.Width(w).Height(h)
	footer := m.styles.hint.Render("PgUp/PgDn: scroll  Home/End: top/bottom")
	content := lipgloss.JoinVertical(lipgloss.Left,
		cmdLine,
		"",
		title,
		"",
		lipgloss.NewStyle().Width(bodyWidth).Height(bodyHeight).Render(body),
		"",
		footer,
	)
	return pane.Render(content)
}

func (m model) rightPaneBodySize(w, h int) (int, int) {
	frameW := m.styles.pane.GetHorizontalFrameSize()
	frameH := m.styles.pane.GetVerticalFrameSize()
	innerW := max(1, w-frameW)
	innerH := max(1, h-frameH)

	cmdLine := lipgloss.NewStyle().
		Width(innerW).
		MaxWidth(innerW).
		Render("Command: " + m.lastCmd)
	title := m.styles.outTitle.Render("I/O")
	footer := m.styles.hint.Render("PgUp/PgDn: scroll  Home/End: top/bottom")

	reservedHeight := lipgloss.Height(cmdLine) + lipgloss.Height(title) + lipgloss.Height(footer) + 3
	bodyH := max(1, innerH-reservedHeight)
	return innerW, bodyH
}

func resolveConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "golaunch", "golaunch.toml"), nil
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	_, err := toml.DecodeFile(path, &cfg)
	return cfg, err
}

func saveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

func wrapOutput(s string, width int) string {
	if width <= 0 {
		return ""
	}
	normalized := normalizeOutputForDisplay(s)
	if normalized == "" {
		return ""
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(normalized)
}

func normalizeOutputForDisplay(s string) string {
	if s == "" {
		return ""
	}

	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return normalized
}

func limitOutputBytes(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}

	prefix := fmt.Sprintf("(output truncated to last %d bytes)\n\n", limit)
	if len(prefix) >= limit {
		return safeUTF8Suffix(s, limit)
	}

	return prefix + safeUTF8Suffix(s, limit-len(prefix))
}

func safeUTF8Suffix(s string, limit int) string {
	if limit <= 0 || s == "" {
		return ""
	}
	if len(s) <= limit {
		return s
	}

	start := len(s) - limit
	for start < len(s) && !utf8.ValidString(s[start:]) {
		start++
	}
	if start >= len(s) {
		return ""
	}
	return s[start:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
