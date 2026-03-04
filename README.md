# golaunch

`golaunch` は Go + Bubble Tea で作った、シンプルな CLI コマンドランチャーです。

左ペインでコマンド一覧や編集を行い、右ペインで「実行コマンド全文」と標準出力/標準エラーを確認できます。

## 主な機能

- コマンド一覧表示（`Current Dir` 表示付き）
- コマンドの追加・編集・複製・削除
- 引数の追加・編集・有効/無効切替・削除
- 一覧画面と編集画面の両方から `r` キーで実行
- 実行中モード（操作ロック + シグナル送信）
- 設定を TOML で永続化

## 要件

- Go 1.22 以上

## 実行方法

```bash
go mod tidy
go run .
```

バイナリ作成:

```bash
go build -o golaunch .
```

## 画面構成

- 左ペイン: 一覧画面 / 編集画面
- 右ペイン:
  - `Command: ...`（実行コマンド全文）
  - `I/O`（`stdout` / `stderr`）

## キー操作

### 共通

- `q`: 終了確認 (`y/n`)
- `Ctrl+C`: 即終了（アプリ終了）

### 一覧画面

- `up/down` (`k/j`): カーソル移動
- `enter`: 編集画面へ
- `r`: 選択中コマンドをそのまま実行
- `a`: 新規コマンド追加（空で編集画面へ）
- `c`: 選択中コマンドを複製（直下に挿入）
- `d`: 選択中コマンドを削除（確認あり）

### 編集画面

- `up/down` (`k/j`): 項目移動
- `enter`: 選択項目を編集
- `r`: 現在の設定で実行（保存して実行）
- `+`: 引数追加
  - 引数行選択中: その直下に挿入
  - それ以外: 末尾追加
- `space`: 選択引数の有効/無効切替（`[*]` / `[ ]`）
- `Ctrl+D` / `del`: 選択中引数を削除
- `esc`: 一覧画面へ戻る

### 実行中モード

コマンド実行中は通常操作をロックします。

- `Ctrl+C` または `i`: 実行中プロセスへ `SIGINT`
- `Ctrl+\\` または `K`: 実行中プロセスへ `SIGKILL`

実行完了後にロック解除され、右ペインに結果が表示されます。

## 設定ファイル

保存先:

```text
~/.config/golaunch/golaunch.toml
```

例:

```toml
[[commands]]
title = "List File"
command = "ls"

  [[commands.args]]
  value = "-l"
  enabled = true

  [[commands.args]]
  value = "."
  enabled = false
```

## ライセンス

MIT License

詳細は [LICENSE](./LICENSE) を参照してください。
