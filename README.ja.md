# codex-history

`codex-history` は、ローカルにある Codex CLI の会話を一覧・全文検索し、
選択した会話をそのまま再開できるTUIです。

非公開の履歴ファイルを直接解析せず、Codexのapp-serverプロトコルを利用します。
会話データとSQLite検索インデックスはローカルに留まります。

## 主な機能

- 会話一覧とトランスクリプトの2ペイン表示（狭い端末では1ペイン）
- ユーザー・Codexメッセージの全文検索
- plan、reasoning summary、コマンド、ファイル変更、MCP呼び出しなどの表示
- `codex resume <thread-id>` による会話の再開
- 厳格かつバージョン管理されたTOML設定
- キーバインド、色、履歴ソース、アーカイブ表示などのカスタマイズ
- macOS / Linux、Homebrew / Nixでの配布を想定した構成

## インストール

Go 1.25以降と、動作する `codex` コマンドが必要です。

```sh
go install github.com/HizKz/codex-history/cmd/codex-history@latest
```

Homebrew tap、nixpkgs、ビルド済みバイナリは最初のタグ付きリリース後に
追加する予定です。

## 使い方

```sh
codex-history
codex-history --reindex
codex-history doctor
codex-history config init
codex-history config check
```

代表的な初期キーバインドは次の通りです。

| キー | 操作 |
| --- | --- |
| `j` / `k`、矢印 | 会話・項目を移動 |
| `tab` | ペイン切り替え |
| `/` | 全文検索 |
| `enter` | 選択した会話を再開 |
| `space` | ツール詳細の展開・折りたたみ |
| `r` / `R` | 再読込 / インデックス再構築 |
| `ctrl+r` | 設定ファイルの再読込 |
| `s` / `a` | 全ソース / アーカイブ表示の切り替え |
| `?` | 設定済みキーを表示 |
| `q` | 終了 |
| `ctrl+c` | 緊急終了（予約キー） |

## 設定

初期パスはOSの標準に従います。

- macOS: `~/Library/Application Support/codex-history/config.toml`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/codex-history/config.toml`

次のコマンドで、全項目を含む設定ファイルを生成できます。

```sh
codex-history config init
```

`CODEX_HISTORY_CONFIG` または `--config` で別のファイルを指定できます。
未知のフィールド、不正な色や値、同時に有効になるキーバインドの競合は起動前に
エラーになります。未指定項目は埋め込み初期値を継承します。キーマップを完全に
作り直す場合は `keys.use_defaults = false` を指定してください。

全設定は [examples/config.toml](examples/config.toml) を参照してください。

SQLiteインデックスには、コマンド出力、MCPの引数・結果などの展開式ツール詳細を
含めません。永続キャッシュを使いたくない場合は `--no-cache` を指定できます。

## 開発

```sh
go test ./...
go vet ./...
staticcheck ./...
nix build --no-link
```

本プロジェクトはコミュニティ製であり、OpenAIの公式製品ではありません。

## ライセンス

MIT — [LICENSE](LICENSE) を参照してください。
