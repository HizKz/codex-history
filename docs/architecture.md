# Architecture

## Data flow

```text
codex app-server (stdio JSON-RPC)
          |
          v
 internal/appserver ---- thread/list, thread/read
          |
          v
 internal/history ------ normalized transcript + privacy-safe search body
          |                         |
          v                         v
 internal/index (SQLite)       internal/tui (Bubble Tea)
                                    |
                                    v
                          codex resume <thread-id>
```

The app-server is the compatibility boundary. The project deliberately avoids
reading Codex's private rollout JSONL and state databases.

## Packages

### `internal/appserver`

Starts `codex app-server --listen stdio://`, performs the initialize handshake,
and multiplexes JSON-RPC responses by request ID. `thread/list` handles paging,
source filters, sort order, and archived conversations. `thread/read` fetches
the complete turns required for rendering and indexing.

The client supports concurrent requests. Writes are serialized, pending
responses are protected by a mutex, and each response channel is completed once.

### `internal/history`

Converts versioned protocol items into a smaller UI model. User and assistant
messages, plans, reasoning summaries, commands, file changes, and tool calls get
stable titles and display fields. Stable `fileChange.changes` entries retain
their path, kind, and unified diff for local rendering.

`Transcript.Body` is a separate privacy boundary for search. It contains
message text and safe descriptions, but excludes command output, MCP payloads,
tool results, file-change paths and diffs, and raw expandable details.

### `internal/index`

Uses a local SQLite database and an FTS5 trigram table. Thread metadata is
upserted transactionally; changed `updated_at` values determine whether a full
thread needs reindexing. Queries of three or more Unicode characters use a
quoted FTS phrase, weighted BM25 ranking, and segmented match snippets. Shorter
queries use escaped `LIKE` patterns. Both paths treat query syntax, `%`, and `_`
as literal user input.

Persistent cache directories and files use user-only permissions where the OS
supports them. Schema version 2 clears version 1 search rows so file-change paths
are removed and conversations become eligible for privacy-safe reindexing.
`--no-cache` uses an in-memory database.

### `internal/config`

Starts from embedded defaults, overlays strict TOML, rejects unknown fields,
and validates all values before the TUI starts. `keys.use_defaults = false`
clears inherited bindings. Explicit bindings override colliding inherited
defaults, while conflicts between explicit bindings are rejected in every
active pane or modal scope. `ctrl+c` remains reserved.

### `internal/tui`

Uses a value model with asynchronous `tea.Cmd` operations for process and disk
I/O. Wide terminals render Conversations, Transcript, and Diff panes together.
Medium terminals show Conversations plus Transcript while the list is focused,
then Transcript plus Diff while either reading pane is focused. Compact
terminals display the focused pane. Conversation entries use a two-line layout
with emphasized titles and subdued working-directory, source, and timestamp
metadata. The selected entry emphasizes both rows as a single block.
During search, the second row displays the ranked result's matched field and
snippet. A separate project picker filters both normal and searched lists by
exact working directory without modifying Codex threads.
The transcript preserves app-server turn boundaries, renders user and final
assistant messages as the primary document, and groups intermediate messages
and tool events into an Activity inspector.
Primary messages use chat-style outlined bubbles: Codex messages align left,
user messages align right, and message text remains left-aligned within each
bubble. Bubbles use at most three quarters of the transcript width. Turn
headings and Activity summaries remain centered system rows, separated from
message bubbles by blank lines.

Transcript navigation uses wrapped logical lines with turn and Activity anchors.
Activity opens an event list, and an event opens a separately scrollable detail
view. Message anchors preserve the current message when a resize changes
wrapping. Wrapping, alignment, and truncation use terminal display width rather
than rune count so Japanese and other full-width text remain safe.

The Diff pane follows the turn under the transcript cursor and concatenates
that turn's file-change entries in protocol order. Diff lines remain unwrapped;
vertical and horizontal viewports keep long source lines and full-width Unicode
inside the terminal boundary. Diff bodies stay outside the search index and are
never recomputed from the current working tree.

Resume modes are:

- `return`: suspend the TUI, run Codex, then refresh.
- `replace` (default): restore the terminal and replace the process with Codex,
  returning to the shell when Codex exits.
- `print_command`: exit and print the resume command.

## Compatibility policy

Protocol-facing changes must be checked against JSON Schema generated by the
installed Codex CLI and covered by the fake app-server test. Prefer stable
fields and methods. Experimental protocol fields require an explicit product
decision and documentation.

Fixtures must be synthetic. Live compatibility checks may report versions,
counts, and success state, but must not print conversation bodies.
