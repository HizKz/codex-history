# codex-history

`codex-history` は、ローカルにある Codex CLI の会話を一覧・全文検索し、
選択した会話をそのまま再開できるTUIです。

非公開の履歴ファイルを直接解析せず、Codexのapp-serverプロトコルを利用します。
会話データとSQLite検索インデックスはローカルに留まります。

## 主な機能

- 会話一覧とターン単位トランスクリプトの2ペイン表示（狭い端末では1ペイン）
- ユーザー・Codexメッセージの関連度順全文検索と一致箇所の表示
- 会話を主役にし、plan、reasoning、コマンド、ファイル変更、MCP呼び出しなどを
  ターンごとのActivityにまとめて表示
- `codex resume <thread-id>` による会話の再開
- 厳格かつバージョン管理されたTOML設定
- キーバインド、色、プロジェクト、履歴ソース、アーカイブ表示などのカスタマイズ
- macOS / Linux、Homebrew / Nixでの配布を想定した構成

## インストール

Codex CLIが `codex` として利用できる必要があります。別の実行ファイルは
`--codex-bin` または `codex.binary` で指定できます。

### Homebrew

```sh
brew install --cask HizKz/tap/codex-history
```

### リリースアーカイブ

[GitHub Releases](https://github.com/HizKz/codex-history/releases) から対象OSの
アーカイブを取得し、`checksums.txt` で検証してからPATH上へ配置してください。

### ソースから

Go 1.25以降と、動作する `codex` コマンドが必要です。

```sh
go install github.com/HizKz/codex-history/cmd/codex-history@latest
```

nixpkgsへの収録は今後対応予定です。

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
| `j` / `k`、矢印 | 会話を移動・本文を1行スクロール |
| `ctrl+u` / `ctrl+d` | 本文・詳細を半画面スクロール |
| `[` / `]` | 前後のターンへ移動 |
| `tab` | ペイン切り替え |
| `/` | 全文検索 |
| `p` | 作業ディレクトリ単位のプロジェクト絞り込み |
| `enter` | 選択した会話を再開 |
| `space` | 選択ターンのActivityを開く |
| `esc` | イベント詳細・Activityから戻る |
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
作り直す場合は `keys.use_defaults = false` を指定してください。明示した
キーバインドは継承された初期値より優先され、明示設定同士の競合は拒否されます。

全設定は [examples/config.toml](examples/config.toml) を参照してください。

SQLiteインデックスには、コマンド出力、MCPの引数・結果などの展開式ツール詳細を
含めません。3文字以上の検索ではtrigramインデックスを使って関連度と一致箇所を
表示し、1〜2文字ではリテラルな部分一致を維持します。永続キャッシュを
使いたくない場合は `--no-cache` を指定できます。

## 開発

```sh
go test ./...
go vet ./...
staticcheck ./...
nix build --no-link
```

リポジトリの作業規約は [AGENTS.md](AGENTS.md)、設計とリリース手順は
[docs/architecture.md](docs/architecture.md) と
[docs/releasing.md](docs/releasing.md) にまとめています。Codexは
`.agents/skills` にある `maintain-codex-history` Skillも自動検出します。

本プロジェクトはコミュニティ製であり、OpenAIの公式製品ではありません。

## ライセンス

MIT — [LICENSE](LICENSE) を参照してください。
