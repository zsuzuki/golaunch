# golaunch

Go + Bubble Tea で作った、シンプルな CLI コマンドランチャーです。

登録済みコマンドを一覧から選んで実行し、標準出力/標準エラーを右ペインで確認できます。

## Features

- コマンド一覧表示（カレントディレクトリ表示付き）
- コマンド追加/編集/削除
- 引数の追加/有効化トグル/削除
- 一覧画面からの即時実行
- 実行コマンド全文と I/O 表示
- 設定を TOML で永続化

## Requirements

- Go 1.22+

## Install / Run

```bash
go mod tidy
go run .
```

ビルド:

```bash
go build -o golaunch .
```

## Key Bindings

### List Screen

- `up/down` (`k/j`): 選択移動
- `enter`: 編集画面へ
- `r`: 選択中コマンドをそのまま実行
- `a`: 新規コマンド追加（空状態で編集画面へ）
- `d`: 選択中コマンド削除（確認あり）
- `q`: 終了確認
- `ctrl+c`: 即終了

### Edit Screen

- `up/down` (`k/j`): 項目移動
- `enter`: 項目編集 / `[Run]` 実行
- `+`: 引数を追加（引数行選択中はその直下に追加）
- `space`: 選択引数の有効/無効トグル
- `ctrl+d` / `del`: 選択引数を削除
- `esc`: 一覧画面へ戻る
- `q`: 終了確認

## Config

設定ファイルは以下に保存されます。

```text
~/.config/golaunch/golaunch.toml
```

フォーマット例:

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

## License

MIT License. See [LICENSE](./LICENSE).
