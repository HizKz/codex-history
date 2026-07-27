package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/config"
	"github.com/HizKz/codex-history/internal/history"
	"github.com/HizKz/codex-history/internal/index"
)

type Options struct {
	Config      config.Config
	ConfigPath  string
	LoadOptions config.LoadOptions
	Index       *index.Store
}

type Outcome struct {
	PrintedCommand string
}

type focus int

const (
	focusList focus = iota
	focusTranscript
)

const conversationListItemRows = 2

type helpSection struct {
	scope string
	title string
}

type model struct {
	ctx              context.Context
	cfg              config.Config
	configPath       string
	loadOptions      config.LoadOptions
	store            *index.Store
	client           *appserver.Client
	threads          []appserver.Thread
	visible          []appserver.Thread
	selected         int
	transcript       history.Transcript
	transcriptID     string
	transcriptView   transcriptView
	transcriptCursor int
	transcriptOffset int
	activityTurn     int
	activityCursor   int
	detailCursor     int
	detailOffset     int
	query            string
	searching        bool
	searchGeneration uint64
	searchResults    []index.Result
	searchMatches    map[string]index.Result
	projectOpen      bool
	projectCursor    int
	projectCWD       string
	focus            focus
	width            int
	height           int
	status           string
	err              string
	showHelp         bool
	loading          bool
	indexing         bool
	allSources       bool
	archived         bool
	printed          string
}

type connectedMsg struct {
	client  *appserver.Client
	threads []appserver.Thread
	err     error
}

type threadsMsg struct {
	threads []appserver.Thread
	err     error
}

type transcriptMsg struct {
	id         string
	transcript history.Transcript
	err        error
}

type searchMsg struct {
	query      string
	generation uint64
	results    []index.Result
	err        error
}

type searchDelayMsg struct {
	query      string
	generation uint64
}

type indexMsg struct {
	count int
	err   error
}

type configMsg struct {
	loaded config.Loaded
	err    error
}

type resumeMsg struct{ err error }
type printCommandMsg string

func Run(ctx context.Context, opts Options) (Outcome, error) {
	m := model{
		ctx:         ctx,
		cfg:         opts.Config,
		configPath:  opts.ConfigPath,
		loadOptions: opts.LoadOptions,
		store:       opts.Index,
		archived:    opts.Config.History.IncludeArchived,
		status:      "Connecting to Codex…",
	}
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if fm, ok := final.(model); ok {
		if fm.client != nil {
			_ = fm.client.Close()
		}
		return Outcome{PrintedCommand: fm.printed}, err
	}
	return Outcome{}, err
}

func (m model) Init() tea.Cmd { return m.connectCmd() }

func (m model) connectCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := appserver.Start(m.ctx, m.cfg.Codex.Binary)
		if err != nil {
			return connectedMsg{err: err}
		}
		threads, err := listThreads(m.ctx, client, m.cfg, m.allSources, m.archived)
		if err != nil {
			_ = client.Close()
			return connectedMsg{err: err}
		}
		return connectedMsg{client: client, threads: threads}
	}
}

func listThreads(ctx context.Context, client *appserver.Client, cfg config.Config, allSources, archived bool) ([]appserver.Thread, error) {
	sources := appserver.SourceKinds(cfg.History.Sources)
	if allSources {
		sources = appserver.AllSourceKinds()
	}
	current, err := client.ListThreads(ctx, appserver.ListOptions{
		SourceKinds: sources, Limit: cfg.History.PageSize, SortKey: cfg.History.SortKey,
		SortDirection: cfg.History.SortDirection,
	})
	if err != nil {
		return nil, err
	}
	if archived {
		old, err := client.ListThreads(ctx, appserver.ListOptions{
			SourceKinds: sources, Archived: true, Limit: cfg.History.PageSize,
			SortKey: cfg.History.SortKey, SortDirection: cfg.History.SortDirection,
		})
		if err != nil {
			return nil, err
		}
		current = append(current, old...)
	}
	sort.SliceStable(current, func(i, j int) bool {
		a, b := sortTime(current[i], cfg.History.SortKey), sortTime(current[j], cfg.History.SortKey)
		if cfg.History.SortDirection == "asc" {
			return a < b
		}
		return a > b
	})
	return current, nil
}

func sortTime(thread appserver.Thread, key string) int64 {
	switch key {
	case "created":
		return thread.CreatedAt
	case "updated":
		return thread.UpdatedAt
	default:
		if thread.RecencyAt != nil {
			return *thread.RecencyAt
		}
		return thread.UpdatedAt
	}
}

func (m model) refreshCmd() tea.Cmd {
	if m.client == nil {
		return m.connectCmd()
	}
	return func() tea.Msg {
		threads, err := listThreads(m.ctx, m.client, m.cfg, m.allSources, m.archived)
		return threadsMsg{threads: threads, err: err}
	}
}

func (m model) loadSelectedCmd() tea.Cmd {
	if m.client == nil || len(m.visible) == 0 || m.selected >= len(m.visible) {
		return nil
	}
	id := m.visible[m.selected].ID
	return func() tea.Msg {
		thread, err := m.client.ReadThread(m.ctx, id)
		if err != nil {
			return transcriptMsg{id: id, err: err}
		}
		return transcriptMsg{id: id, transcript: history.Build(thread)}
	}
}

func (m model) indexCmd(clear bool) tea.Cmd {
	threads := append([]appserver.Thread(nil), m.threads...)
	return func() tea.Msg {
		if clear {
			if err := m.store.Clear(m.ctx); err != nil {
				return indexMsg{err: err}
			}
		}
		workers := m.cfg.Search.MaxParallelReads
		jobs := make(chan appserver.Thread)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var firstErr error
		count := 0
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for summary := range jobs {
					needed, err := m.store.NeedsIndex(m.ctx, summary.ID, summary.UpdatedAt)
					if err == nil && needed {
						var full appserver.Thread
						full, err = m.client.ReadThread(m.ctx, summary.ID)
						if err == nil {
							full.Archived = summary.Archived
							err = m.store.Upsert(m.ctx, full, history.Build(full))
						}
					}
					mu.Lock()
					if err != nil && firstErr == nil {
						firstErr = err
					}
					if err == nil && needed {
						count++
					}
					mu.Unlock()
				}
			}()
		}
		for _, thread := range threads {
			jobs <- thread
		}
		close(jobs)
		wg.Wait()
		return indexMsg{count: count, err: firstErr}
	}
}

func (m model) searchCmd() tea.Cmd {
	query := m.query
	generation := m.searchGeneration
	return func() tea.Msg {
		results, err := m.store.Search(m.ctx, query, 500)
		return searchMsg{query: query, generation: generation, results: results, err: err}
	}
}

func (m model) delayedSearchCmd() tea.Cmd {
	query := m.query
	generation := m.searchGeneration
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return searchDelayMsg{query: query, generation: generation}
	})
}

func (m model) reloadConfigCmd() tea.Cmd {
	return func() tea.Msg {
		loaded, err := config.Load(m.loadOptions)
		return configMsg{loaded: loaded, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldLines := m.currentTranscriptLines()
		oldCursor := min(max(m.transcriptCursor, 0), max(0, len(oldLines)-1))
		m.width, m.height = msg.Width, msg.Height
		if m.transcriptView == transcriptConversation && len(oldLines) > 0 {
			m.transcriptCursor = resizedAnchorLine(m.currentTranscriptLines(), oldLines[oldCursor])
			m.clampTranscriptCursor(m.currentTranscriptLines())
		}
	case connectedMsg:
		m.loading = false
		if msg.err != nil {
			m.err, m.status = msg.err.Error(), "Unable to connect"
			return m, nil
		}
		m.client, m.threads = msg.client, msg.threads
		m.applyCurrentFilters()
		m.status = fmt.Sprintf("%d conversations", len(m.threads))
		m.clampSelection()
		cmds := []tea.Cmd{m.loadSelectedCmd()}
		if m.cfg.Search.IndexOnStartup {
			m.indexing = true
			cmds = append(cmds, m.indexCmd(false))
		}
		return m, tea.Batch(cmds...)
	case threadsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.threads, m.query, m.searchResults, m.searchMatches = msg.threads, "", nil, nil
		m.searchGeneration++
		projectCleared := m.clearMissingProject()
		m.applyCurrentFilters()
		m.status = fmt.Sprintf("%d conversations", len(m.threads))
		if projectCleared {
			m.status = "Project filter cleared; " + m.status
		}
		m.clampSelection()
		return m, m.loadSelectedCmd()
	case transcriptMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		if len(m.visible) > 0 && m.visible[m.selected].ID == msg.id {
			m.transcript, m.transcriptID = msg.transcript, msg.id
			m.resetTranscriptView()
		}
		if m.store != nil {
			_ = m.store.Upsert(m.ctx, msg.transcript.Thread, msg.transcript)
		}
	case searchMsg:
		if msg.query != m.query || msg.generation != m.searchGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.searchResults = msg.results
		m.applyCurrentFilters()
		m.clampSelection()
		return m, m.loadSelectedCmd()
	case searchDelayMsg:
		if msg.query != m.query || msg.generation != m.searchGeneration || msg.query == "" {
			return m, nil
		}
		return m, m.searchCmd()
	case indexMsg:
		m.indexing = false
		if msg.err != nil {
			m.err = "Indexing: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("Indexed %d changed conversations", msg.count)
		}
		if m.query != "" {
			return m, m.searchCmd()
		}
	case configMsg:
		if msg.err != nil {
			m.err = "Config unchanged: " + msg.err.Error()
			return m, nil
		}
		binaryChanged := m.cfg.Codex.Binary != msg.loaded.Config.Codex.Binary
		m.cfg, m.configPath = msg.loaded.Config, msg.loaded.Path
		m.status, m.err = "Configuration reloaded", ""
		if binaryChanged && m.client != nil {
			_ = m.client.Close()
			m.client = nil
		}
		return m, m.refreshCmd()
	case resumeMsg:
		if msg.err != nil {
			m.err = "Resume failed: " + msg.err.Error()
		} else {
			m.status = "Returned from Codex; refreshing…"
			return m, m.refreshCmd()
		}
	case printCommandMsg:
		m.printed = string(msg)
		return m, tea.Quit
	case tea.PasteMsg:
		if m.searching {
			m.query += strings.ReplaceAll(msg.Content, "\n", " ")
			m.searchGeneration++
			return m, m.delayedSearchCmd()
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Keystroke()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.projectOpen {
		return m.handleProjectKey(key)
	}
	if m.searching {
		switch {
		case m.cfg.Keys.Match("search", "accept", key):
			m.searching, m.status = false, fmt.Sprintf("%d matches", len(m.visible))
			return m, nil
		case m.cfg.Keys.Match("search", "cancel", key):
			m.searching, m.query, m.searchResults, m.searchMatches = false, "", nil, nil
			m.searchGeneration++
			m.applyCurrentFilters()
			m.clampSelection()
			return m, m.loadSelectedCmd()
		case m.cfg.Keys.Match("search", "clear", key):
			m.query, m.searchResults, m.searchMatches = "", nil, nil
			m.searchGeneration++
			m.applyCurrentFilters()
			m.clampSelection()
			return m, m.loadSelectedCmd()
		case key == "backspace":
			if len(m.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(m.query)
				m.query = m.query[:len(m.query)-size]
			}
			if m.query == "" {
				m.searchResults, m.searchMatches = nil, nil
				m.searchGeneration++
				m.applyCurrentFilters()
				m.clampSelection()
				return m, m.loadSelectedCmd()
			}
			m.searchGeneration++
			return m, m.delayedSearchCmd()
		default:
			if text := msg.Key().Text; text != "" {
				m.query += text
				m.searchGeneration++
				return m, m.delayedSearchCmd()
			}
		}
		return m, nil
	}
	if m.cfg.Keys.Match("global", "quit", key) {
		return m, tea.Quit
	}
	if m.cfg.Keys.Match("global", "help", key) {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if m.showHelp {
		return m, nil
	}
	if m.cfg.Keys.Match("global", "filter_project", key) {
		m.openProjectPicker()
		return m, nil
	}
	if m.focus == focusTranscript && m.transcriptView != transcriptConversation {
		return m.handleTranscriptKey(key)
	}
	switch {
	case m.cfg.Keys.Match("global", "focus_next", key):
		m.focus = (m.focus + 1) % 2
	case m.cfg.Keys.Match("global", "search", key):
		m.searching = true
	case m.cfg.Keys.Match("global", "refresh", key):
		m.loading, m.status = true, "Refreshing…"
		return m, m.refreshCmd()
	case m.cfg.Keys.Match("global", "rebuild_index", key):
		m.indexing, m.status = true, "Rebuilding search index…"
		return m, m.indexCmd(true)
	case m.cfg.Keys.Match("global", "reload_config", key):
		m.status = "Reloading configuration…"
		return m, m.reloadConfigCmd()
	case m.cfg.Keys.Match("global", "toggle_sources", key):
		m.allSources, m.loading = !m.allSources, true
		return m, m.refreshCmd()
	case m.cfg.Keys.Match("global", "toggle_archived", key):
		m.archived, m.loading = !m.archived, true
		return m, m.refreshCmd()
	default:
		if m.focus == focusList {
			return m.handleListKey(key)
		}
		return m.handleTranscriptKey(key)
	}
	return m, nil
}

func (m model) handleListKey(key string) (tea.Model, tea.Cmd) {
	old := m.selected
	page := conversationListPageSize(m.transcriptRows())
	switch {
	case m.cfg.Keys.Match("list", "up", key):
		m.selected--
	case m.cfg.Keys.Match("list", "down", key):
		m.selected++
	case m.cfg.Keys.Match("list", "page_up", key):
		m.selected -= page
	case m.cfg.Keys.Match("list", "page_down", key):
		m.selected += page
	case m.cfg.Keys.Match("list", "first", key):
		m.selected = 0
	case m.cfg.Keys.Match("list", "last", key):
		m.selected = len(m.visible) - 1
	case m.cfg.Keys.Match("list", "resume", key):
		return m, m.resumeCmd()
	}
	m.clampSelection()
	if old != m.selected {
		m.resetTranscriptView()
		return m, m.loadSelectedCmd()
	}
	return m, nil
}

func (m model) handleTranscriptKey(key string) (tea.Model, tea.Cmd) {
	switch m.transcriptView {
	case transcriptActivity:
		return m.handleActivityKey(key)
	case transcriptDetail:
		return m.handleDetailKey(key)
	}

	lines := m.currentTranscriptLines()
	page := max(1, m.transcriptRows()/2)
	switch {
	case m.cfg.Keys.Match("transcript", "up", key):
		m.transcriptCursor--
	case m.cfg.Keys.Match("transcript", "down", key):
		m.transcriptCursor++
	case m.cfg.Keys.Match("transcript", "page_up", key):
		m.transcriptCursor -= page
	case m.cfg.Keys.Match("transcript", "page_down", key):
		m.transcriptCursor += page
	case m.cfg.Keys.Match("transcript", "previous_turn", key):
		m.transcriptCursor = previousTurnLine(lines, m.transcriptCursor)
	case m.cfg.Keys.Match("transcript", "next_turn", key):
		m.transcriptCursor = nextTurnLine(lines, m.transcriptCursor)
	case m.cfg.Keys.Match("transcript", "toggle_item", key):
		if m.transcriptCursor >= 0 && m.transcriptCursor < len(lines) && lines[m.transcriptCursor].Actionable {
			m.transcriptView = transcriptActivity
			m.activityTurn = lines[m.transcriptCursor].Turn
			m.activityCursor = 0
		}
	}
	m.clampTranscriptCursor(lines)
	return m, nil
}

func (m model) handleActivityKey(key string) (tea.Model, tea.Cmd) {
	items := m.currentActivity()
	switch {
	case m.cfg.Keys.Match("activity", "close", key):
		m.transcriptView = transcriptConversation
	case m.cfg.Keys.Match("activity", "up", key):
		m.activityCursor--
	case m.cfg.Keys.Match("activity", "down", key):
		m.activityCursor++
	case m.cfg.Keys.Match("activity", "open", key):
		if len(items) > 0 {
			m.activityCursor = min(max(m.activityCursor, 0), len(items)-1)
			m.transcriptView = transcriptDetail
			m.detailCursor, m.detailOffset = 0, 0
		}
	}
	if len(items) == 0 {
		m.activityCursor = 0
	} else {
		m.activityCursor = min(max(m.activityCursor, 0), len(items)-1)
	}
	return m, nil
}

func (m model) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	lines := m.currentDetailLines()
	page := max(1, m.transcriptRows()/2)
	switch {
	case m.cfg.Keys.Match("detail", "close", key):
		m.transcriptView = transcriptActivity
	case m.cfg.Keys.Match("detail", "up", key):
		m.detailCursor--
	case m.cfg.Keys.Match("detail", "down", key):
		m.detailCursor++
	case m.cfg.Keys.Match("detail", "page_up", key):
		m.detailCursor -= page
	case m.cfg.Keys.Match("detail", "page_down", key):
		m.detailCursor += page
	}
	if len(lines) == 0 {
		m.detailCursor = 0
	} else {
		m.detailCursor = min(max(m.detailCursor, 0), len(lines)-1)
	}
	m.detailOffset = visibleStart(m.detailOffset, m.detailCursor, m.transcriptRows(), len(lines))
	return m, nil
}

func (m model) resumeCmd() tea.Cmd {
	if len(m.visible) == 0 || m.selected >= len(m.visible) {
		return nil
	}
	id := m.visible[m.selected].ID
	args := []string{"resume", id}
	command := shellCommand(m.cfg.Codex.Binary, args...)
	switch m.cfg.Resume.Mode {
	case "print_command":
		return func() tea.Msg { return printCommandMsg(command) }
	case "replace":
		return tea.Exec(&replaceCommand{binary: m.cfg.Codex.Binary, args: args}, func(err error) tea.Msg {
			return resumeMsg{err: err}
		})
	default:
		return tea.ExecProcess(exec.Command(m.cfg.Codex.Binary, args...), func(err error) tea.Msg {
			return resumeMsg{err: err}
		})
	}
}

type replaceCommand struct {
	binary string
	args   []string
}

func (c *replaceCommand) Run() error {
	path, err := exec.LookPath(c.binary)
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{c.binary}, c.args...), os.Environ())
}
func (*replaceCommand) SetStdin(io.Reader)  {}
func (*replaceCommand) SetStdout(io.Writer) {}
func (*replaceCommand) SetStderr(io.Writer) {}

func shellCommand(binary string, args ...string) string {
	parts := append([]string{binary}, args...)
	for i, part := range parts {
		if strings.ContainsAny(part, " \t\n'\"") {
			parts[i] = "'" + strings.ReplaceAll(part, "'", "'\\''") + "'"
		}
	}
	return strings.Join(parts, " ")
}

func (m *model) clampSelection() {
	if len(m.visible) == 0 {
		m.selected = 0
		return
	}
	m.selected = min(max(m.selected, 0), len(m.visible)-1)
}

func (m *model) resetTranscriptView() {
	m.transcriptView = transcriptConversation
	m.transcriptCursor = 0
	m.transcriptOffset = 0
	m.activityTurn = 0
	m.activityCursor = 0
	m.detailCursor = 0
	m.detailOffset = 0
}

func (m model) compactLayout() bool {
	return max(m.width, 60) < m.cfg.UI.CompactBreakpoint
}

func (m model) transcriptPanelWidth() int {
	width := max(m.width, 60)
	if m.compactLayout() {
		return width
	}
	leftWidth := max(30, min(48, width*38/100))
	return max(30, width-leftWidth-1)
}

func (m model) transcriptContentWidth() int {
	return max(4, m.transcriptPanelWidth()-6)
}

func (m model) transcriptRows() int {
	height := max(m.height, 16)
	bodyHeight := max(5, height-4)
	innerHeight := max(2, bodyHeight-2)
	return max(1, innerHeight-1)
}

func (m model) currentTranscriptLines() []transcriptLine {
	return buildTranscriptLines(
		m.transcript,
		m.transcriptContentWidth(),
		m.cfg.UI.ToolDetails == "expanded",
	)
}

func (m *model) clampTranscriptCursor(lines []transcriptLine) {
	if len(lines) == 0 {
		m.transcriptCursor, m.transcriptOffset = 0, 0
		return
	}
	m.transcriptCursor = min(max(m.transcriptCursor, 0), len(lines)-1)
	m.transcriptOffset = visibleStart(
		m.transcriptOffset,
		m.transcriptCursor,
		m.transcriptRows(),
		len(lines),
	)
}

func (m model) currentActivity() []history.Item {
	if m.activityTurn < 0 || m.activityTurn >= len(m.transcript.Turns) {
		return nil
	}
	return m.transcript.Turns[m.activityTurn].Activity
}

func (m model) currentDetailLines() []string {
	items := m.currentActivity()
	if m.activityCursor < 0 || m.activityCursor >= len(items) {
		return nil
	}
	return detailLines(items[m.activityCursor], m.transcriptContentWidth())
}

func previousTurnLine(lines []transcriptLine, cursor int) int {
	if len(lines) == 0 {
		return 0
	}
	cursor = min(max(cursor, 0), len(lines)-1)
	currentTurn := lines[cursor].Turn
	for i := cursor; i >= 0; i-- {
		if lines[i].TurnStart && lines[i].Turn < currentTurn {
			return i
		}
	}
	return 0
}

func nextTurnLine(lines []transcriptLine, cursor int) int {
	if len(lines) == 0 {
		return 0
	}
	cursor = min(max(cursor, 0), len(lines)-1)
	currentTurn := lines[cursor].Turn
	for i := cursor + 1; i < len(lines); i++ {
		if lines[i].TurnStart && lines[i].Turn > currentTurn {
			return i
		}
	}
	return len(lines) - 1
}

func resizedAnchorLine(lines []transcriptLine, anchor transcriptLine) int {
	if anchor.Anchor != "" {
		for i, line := range lines {
			if line.Anchor == anchor.Anchor && line.Kind == anchor.Kind {
				return i
			}
		}
		for i, line := range lines {
			if line.Anchor == anchor.Anchor {
				return i
			}
		}
	}
	for i, line := range lines {
		if line.Turn == anchor.Turn && line.Activity == anchor.Activity && line.Role == anchor.Role {
			return i
		}
	}
	for i, line := range lines {
		if line.Turn == anchor.Turn && line.TurnStart {
			return i
		}
	}
	return 0
}

func (m *model) applyCurrentFilters() {
	if m.query == "" {
		m.searchMatches = nil
		m.visible = m.visibleForProject(m.threads)
		return
	}
	m.applySearch(m.searchResults)
}

func (m *model) applySearch(results []index.Result) {
	byID := make(map[string]appserver.Thread, len(m.threads))
	for _, thread := range m.threads {
		byID[thread.ID] = thread
	}
	seen := make(map[string]bool)
	visible := make([]appserver.Thread, 0, len(results))
	m.searchMatches = make(map[string]index.Result, len(results))
	for _, result := range results {
		if thread, ok := byID[result.ID]; ok && m.matchesProject(thread) {
			visible = append(visible, thread)
			seen[result.ID] = true
			m.searchMatches[result.ID] = result
		}
	}
	needle := strings.ToLower(strings.TrimSpace(m.query))
	for _, thread := range m.threads {
		if !m.matchesProject(thread) {
			continue
		}
		metadata := history.Title(thread) + "\n" + thread.Preview + "\n" + thread.CWD
		if !seen[thread.ID] && strings.Contains(strings.ToLower(metadata), needle) {
			visible = append(visible, thread)
			m.searchMatches[thread.ID] = metadataSearchResult(thread, m.query)
		}
	}
	m.visible = visible
}

func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m model) render() string {
	if m.projectOpen {
		return m.renderProjectPicker()
	}
	if m.showHelp {
		return m.renderHelp()
	}
	width := max(m.width, 60)
	height := max(m.height, 16)
	header := m.renderHeader(width)
	bodyHeight := max(5, height-4)
	var body string
	if width < m.cfg.UI.CompactBreakpoint {
		if m.focus == focusList {
			body = m.renderList(width, bodyHeight)
		} else {
			body = m.renderTranscript(width, bodyHeight)
		}
	} else {
		leftWidth := max(30, min(48, width*38/100))
		rightWidth := max(30, width-leftWidth-1)
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderList(leftWidth, bodyHeight),
			m.renderTranscript(rightWidth, bodyHeight),
		)
	}
	footerStyle := m.mutedStyle()
	if m.err != "" {
		footerStyle = applyForeground(footerStyle, m.semanticColor("error", m.cfg.UI.Colors.Error))
	}
	footer := footerStyle.MaxWidth(width).Render(m.footerText())
	return header + "\n" + body + "\n" + footer
}

func (m model) renderHeader(width int) string {
	title := m.accentStyle().Bold(true).Render("codex-history")
	mode := fmt.Sprintf(
		"sources %s  ·  archived %s  ·  project %s",
		map[bool]string{true: "all", false: "config"}[m.allSources],
		map[bool]string{true: "on", false: "off"}[m.archived],
		m.projectLabel(),
	)
	if m.indexing {
		mode += "  ·  indexing…"
	}
	search := ""
	if m.searching || m.query != "" {
		search = "  ·  search: " + m.query
		if m.searching {
			search += "▌"
		}
	}
	line := title + "  " + m.mutedStyle().Render(mode) + search
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}

func (m model) renderList(width, height int) string {
	active := m.focus == focusList
	name := "Conversations"
	if m.compactLayout() {
		name += " 1/2"
	}
	title := panelTitle(name, active, fmt.Sprintf("%d", len(m.visible)))
	innerWidth := max(10, width-2)
	rowWidth := max(4, innerWidth-2)
	contentWidth := max(1, rowWidth-2)
	innerHeight := max(2, height-2)
	rows := max(1, innerHeight-1)
	pageSize := conversationListPageSize(rows)
	start := 0
	if m.selected >= pageSize {
		start = m.selected - pageSize + 1
	}
	end := min(len(m.visible), start+pageSize)
	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		thread := m.visible[i]
		marker := "  "
		if i == m.selected {
			marker = "› "
		}
		itemTitle := history.Title(thread)
		if thread.Archived {
			itemTitle = "[A] " + itemTitle
		}
		title := marker + truncate(itemTitle, contentWidth)
		metadataMarker := "  "
		selected := i == m.selected
		titleStyle := m.conversationListTitleStyle(selected).Width(rowWidth).MaxWidth(rowWidth)
		metadataStyle := m.conversationListMetadataStyle(selected).Width(rowWidth).MaxWidth(rowWidth)
		if i == m.selected {
			metadataMarker = "│ "
		}
		if result, ok := m.searchMatches[thread.ID]; ok && m.query != "" {
			metadata := m.conversationListMetadataStyle(selected).Render(metadataMarker) +
				m.renderSearchContext(result, contentWidth, selected)
			lines = append(lines, titleStyle.Render(title), lipgloss.NewStyle().Width(rowWidth).MaxWidth(rowWidth).Render(metadata))
		} else {
			metadata := metadataMarker + truncate(m.conversationListMetadata(thread), contentWidth)
			lines = append(lines, titleStyle.Render(title), metadataStyle.Render(metadata))
		}
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + strings.Join(lines, "\n"))
}

func conversationListPageSize(rows int) int {
	return max(1, rows/conversationListItemRows)
}

func (m model) conversationListTitleStyle(selected bool) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if selected {
		style = applyForeground(style, m.semanticColor("selected", m.cfg.UI.Colors.Selected)).Underline(true)
	}
	return style
}

func (m model) conversationListMetadataStyle(selected bool) lipgloss.Style {
	if selected {
		return applyForeground(lipgloss.NewStyle(), m.semanticColor("selected", m.cfg.UI.Colors.Selected))
	}
	return m.mutedStyle().Faint(true)
}

func (m model) conversationListMetadata(thread appserver.Thread) string {
	var parts []string
	if cwd := strings.TrimSpace(thread.CWD); cwd != "" {
		parts = append(parts, filepath.Base(cwd))
	}
	if source := strings.TrimSpace(thread.Source); source != "" {
		parts = append(parts, source)
	}
	if m.cfg.UI.ShowTimestamps {
		parts = append(parts, time.Unix(thread.UpdatedAt, 0).Format(m.cfg.UI.DateFormat))
	}
	return strings.Join(parts, " · ")
}

func (m model) renderTranscript(width, height int) string {
	active := m.focus == focusTranscript
	innerWidth := max(10, width-2)
	innerHeight := max(2, height-2)
	rows := max(1, innerHeight-1)
	if m.transcriptID == "" {
		name := "Transcript"
		if m.compactLayout() {
			name += " 2/2"
		}
		title := panelTitle(name, active, "")
		return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + m.mutedStyle().Render("Select a conversation"))
	}
	switch m.transcriptView {
	case transcriptActivity:
		return m.renderActivityPanel(active, innerWidth, innerHeight, rows)
	case transcriptDetail:
		return m.renderDetailPanel(active, innerWidth, innerHeight, rows)
	default:
		return m.renderConversationPanel(active, innerWidth, innerHeight, rows)
	}
}

func (m model) renderConversationPanel(active bool, innerWidth, innerHeight, rows int) string {
	rowWidth := max(4, innerWidth-2)
	contentWidth := max(4, rowWidth-2)
	logical := buildTranscriptLines(m.transcript, contentWidth, m.cfg.UI.ToolDetails == "expanded")
	cursor := 0
	if len(logical) > 0 {
		cursor = min(max(m.transcriptCursor, 0), len(logical)-1)
	}
	start := visibleStart(m.transcriptOffset, cursor, rows, len(logical))
	end := min(len(logical), start+rows)
	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		line := logical[i]
		gutter := "  "
		if i == cursor {
			gutter = m.accentStyle().Bold(true).Render("▌ ")
		}
		row := m.renderTranscriptLine(line, contentWidth)
		lines = append(lines, lipgloss.NewStyle().Width(rowWidth).MaxWidth(rowWidth).Render(gutter+row))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	name := "Transcript"
	if m.compactLayout() {
		name += " 2/2"
	}
	position := "0/0"
	if len(logical) > 0 {
		position = fmt.Sprintf("%d/%d", cursor+1, len(logical))
	}
	title := panelTitle(name, active, position)
	return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + strings.Join(lines, "\n"))
}

func (m model) renderTranscriptLine(line transcriptLine, width int) string {
	width = max(1, width)
	var rendered string
	switch line.Kind {
	case lineBlank:
		return ""
	case lineTurnDivider:
		label := truncate(line.Text, max(1, width-2))
		remaining := max(0, width-lipgloss.Width(label)-2)
		left := remaining / 2
		right := remaining - left
		rendered = m.mutedStyle().Render(
			strings.Repeat("─", left) + " " + label + " " + strings.Repeat("─", right),
		)
	case lineBubbleTop, lineBubbleBody, lineBubbleBottom:
		rendered = m.renderMessageBubbleLine(line, width)
	default:
		style := lipgloss.NewStyle()
		switch line.Role {
		case lineMuted:
			style = m.mutedStyle()
		case lineUser:
			style = applyForeground(style, m.semanticColor("user", m.cfg.UI.Colors.User)).Bold(true)
		case lineAssistant:
			style = applyForeground(style, m.semanticColor("assistant", m.cfg.UI.Colors.Assistant)).Bold(true)
		case lineActivity:
			style = m.accentStyle().Bold(true)
		}
		rendered = style.Render(truncate(line.Text, width))
	}

	renderedWidth := lipgloss.Width(rendered)
	switch line.Alignment {
	case alignCenter:
		return strings.Repeat(" ", max(0, (width-renderedWidth)/2)) + rendered
	case alignRight:
		return strings.Repeat(" ", max(0, width-renderedWidth)) + rendered
	default:
		return rendered
	}
}

func (m model) renderMessageBubbleLine(line transcriptLine, availableWidth int) string {
	bubbleWidth := min(max(4, line.BlockWidth), availableWidth)
	borderStyle := lipgloss.NewStyle()
	switch line.Role {
	case lineUser:
		borderStyle = applyForeground(borderStyle, m.semanticColor("user", m.cfg.UI.Colors.User)).Bold(true)
	case lineAssistant:
		borderStyle = applyForeground(borderStyle, m.semanticColor("assistant", m.cfg.UI.Colors.Assistant)).Bold(true)
	}

	switch line.Kind {
	case lineBubbleTop:
		edgeWidth := max(2, bubbleWidth-2)
		edge := strings.Repeat("─", edgeWidth)
		if edgeWidth > 2 {
			label := " " + truncate(line.Text, edgeWidth-2) + " "
			labelWidth := lipgloss.Width(label)
			fill := strings.Repeat("─", max(0, edgeWidth-labelWidth))
			edge = label + fill
			if line.Alignment == alignRight {
				edge = fill + label
			}
		}
		return borderStyle.Render("╭" + edge + "╮")
	case lineBubbleBottom:
		return borderStyle.Render("╰" + strings.Repeat("─", max(0, bubbleWidth-2)) + "╯")
	default:
		textWidth := max(0, bubbleWidth-4)
		text := line.Text
		if lipgloss.Width(text) > textWidth {
			text, _ = splitDisplayWidth(text, textWidth)
		}
		body := lipgloss.NewStyle().Width(textWidth).MaxWidth(textWidth).Render(text)
		return borderStyle.Render("│") + " " + body + " " + borderStyle.Render("│")
	}
}

func (m model) renderActivityPanel(active bool, innerWidth, innerHeight, rows int) string {
	rowWidth := max(4, innerWidth-2)
	items := m.currentActivity()
	cursor := 0
	if len(items) > 0 {
		cursor = min(max(m.activityCursor, 0), len(items)-1)
	}
	start := visibleStart(0, cursor, rows, len(items))
	end := min(len(items), start+rows)
	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		gutter := "  "
		style := lipgloss.NewStyle()
		if i == cursor {
			gutter = "› "
			style = m.accentStyle().Bold(true)
		}
		text := truncate(activityEventLabel(i, items[i]), max(4, rowWidth-2))
		lines = append(lines, lipgloss.NewStyle().Width(rowWidth).MaxWidth(rowWidth).Render(gutter+style.Render(text)))
	}
	if len(items) == 0 {
		lines = append(lines, m.mutedStyle().Render("No activity"))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	title := panelTitle(
		fmt.Sprintf("Activity · Turn %d", m.activityTurn+1),
		active,
		fmt.Sprintf("%d", len(items)),
	)
	return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + strings.Join(lines, "\n"))
}

func (m model) renderDetailPanel(active bool, innerWidth, innerHeight, rows int) string {
	rowWidth := max(4, innerWidth-2)
	items := m.currentActivity()
	if len(items) == 0 {
		title := panelTitle("Detail", active, "")
		return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + m.mutedStyle().Render("No details"))
	}
	itemIndex := min(max(m.activityCursor, 0), len(items)-1)
	logical := detailLines(items[itemIndex], max(4, rowWidth-2))
	cursor := min(max(m.detailCursor, 0), max(0, len(logical)-1))
	start := visibleStart(m.detailOffset, cursor, rows, len(logical))
	end := min(len(logical), start+rows)
	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		gutter := "  "
		if i == cursor {
			gutter = m.accentStyle().Bold(true).Render("▌ ")
		}
		lines = append(lines, lipgloss.NewStyle().Width(rowWidth).MaxWidth(rowWidth).Render(gutter+logical[i]))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	position := fmt.Sprintf("%d/%d", cursor+1, max(1, len(logical)))
	title := panelTitle("Detail · "+truncate(items[itemIndex].Title, max(8, innerWidth-20)), active, position)
	return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + strings.Join(lines, "\n"))
}

func panelTitle(name string, active bool, extra string) string {
	prefix := "  "
	if active {
		prefix = "▶ "
	}
	if extra != "" {
		return fmt.Sprintf("%s%s · %s ", prefix, name, extra)
	}
	return prefix + name + " "
}

func (m model) renderHelp() string {
	var lines []string
	lines = append(lines, m.accentStyle().Bold(true).Render("codex-history key bindings"), "")
	bindings := m.cfg.Keys.Bindings()
	for _, section := range m.helpSections() {
		var sectionLines []string
		for _, binding := range bindings {
			if binding.Scope != section.scope || len(binding.Keys) == 0 {
				continue
			}
			sectionLines = append(sectionLines, fmt.Sprintf("%-24s %s", bindingLabel(binding.Action), strings.Join(binding.Keys, ", ")))
		}
		if len(sectionLines) > 0 {
			lines = append(lines, section.title)
			lines = append(lines, sectionLines...)
			lines = append(lines, "")
		}
	}
	lines = append(lines, "ctrl+c is always available as an emergency exit.", "Press the help key to close.")
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m model) helpSections() []helpSection {
	sections := []helpSection{{"global", "GLOBAL"}}
	if m.focus == focusList {
		return append(sections,
			helpSection{"list", "CONVERSATIONS"},
			helpSection{"search", "SEARCH"},
		)
	}
	switch m.transcriptView {
	case transcriptActivity:
		return append(sections, helpSection{"activity", "ACTIVITY"})
	case transcriptDetail:
		return append(sections, helpSection{"detail", "DETAIL"})
	default:
		return append(sections, helpSection{"transcript", "TRANSCRIPT"})
	}
}

func (m model) footerText() string {
	if m.err != "" {
		return "error: " + m.err
	}
	if m.searching {
		return strings.Join(compactHints(
			"type to search",
			m.keyHint("search", "accept", "accept"),
			m.keyHint("search", "cancel", "cancel"),
			m.keyHint("search", "clear", "clear"),
		), " • ")
	}
	if !m.cfg.UI.ShowHelp {
		return m.status
	}
	var hints []string
	if m.focus == focusList {
		hints = compactHints(
			m.keyPairHint("list", "down", "up", "move"),
			m.keyHint("list", "resume", "resume"),
			m.keyHint("global", "focus_next", "transcript"),
			m.keyHint("global", "search", "search"),
			m.keyHint("global", "filter_project", "project"),
			m.keyHint("global", "help", "help"),
		)
	} else {
		switch m.transcriptView {
		case transcriptActivity:
			hints = compactHints(
				m.keyPairHint("activity", "down", "up", "move"),
				m.keyHint("activity", "open", "open"),
				m.keyHint("activity", "close", "back"),
				m.keyHint("global", "help", "help"),
			)
		case transcriptDetail:
			hints = compactHints(
				m.keyPairHint("detail", "down", "up", "scroll"),
				m.keyPairHint("detail", "page_up", "page_down", "page"),
				m.keyHint("detail", "close", "back"),
				m.keyHint("global", "help", "help"),
			)
		default:
			hints = compactHints(
				m.keyPairHint("transcript", "down", "up", "scroll"),
				m.keyPairHint("transcript", "page_up", "page_down", "page"),
				m.keyPairHint("transcript", "previous_turn", "next_turn", "turn"),
				m.keyHint("transcript", "toggle_item", "activity"),
				m.keyHint("global", "focus_next", "list"),
				m.keyHint("global", "help", "help"),
			)
		}
	}
	actions := strings.Join(hints, " • ")
	if m.status == "" {
		return actions
	}
	return m.status + "  •  " + actions
}

func (m model) keyHint(scope, action, label string) string {
	keys := m.cfg.Keys.KeysFor(scope, action)
	if len(keys) == 0 {
		return ""
	}
	return keys[0] + " " + label
}

func (m model) keyPairHint(scope, firstAction, secondAction, label string) string {
	first := m.cfg.Keys.KeysFor(scope, firstAction)
	second := m.cfg.Keys.KeysFor(scope, secondAction)
	if len(first) == 0 || len(second) == 0 {
		return ""
	}
	return first[0] + "/" + second[0] + " " + label
}

func compactHints(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func bindingLabel(action string) string {
	labels := map[string]string{
		"focus_next":      "Switch pane",
		"rebuild_index":   "Rebuild index",
		"reload_config":   "Reload config",
		"toggle_sources":  "Toggle sources",
		"toggle_archived": "Toggle archived",
		"filter_project":  "Filter project",
		"page_up":         "Page up",
		"page_down":       "Page down",
		"previous_turn":   "Previous turn",
		"next_turn":       "Next turn",
		"toggle_item":     "Open activity",
	}
	if label := labels[action]; label != "" {
		return label
	}
	return strings.ToUpper(action[:1]) + strings.ReplaceAll(action[1:], "_", " ")
}

func (m model) panelStyle(active bool) lipgloss.Style {
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	if active {
		return applyForeground(style, m.semanticColor("accent", m.cfg.UI.Colors.Accent))
	}
	return applyForeground(style, m.semanticColor("border", m.cfg.UI.Colors.Border))
}

func (m model) accentStyle() lipgloss.Style {
	return applyForeground(lipgloss.NewStyle(), m.semanticColor("accent", m.cfg.UI.Colors.Accent))
}
func (m model) mutedStyle() lipgloss.Style {
	return applyForeground(lipgloss.NewStyle(), m.semanticColor("muted", m.cfg.UI.Colors.Muted))
}

func (m model) semanticColor(name, configured string) string {
	if configured != "" && configured != "default" {
		return configured
	}
	if m.cfg.UI.Theme == "terminal" {
		return "default"
	}
	palettes := map[string]map[string]string{
		"dark": {
			"accent": "bright_cyan", "selected": "bright_white", "muted": "bright_black",
			"border": "bright_black", "user": "green", "assistant": "cyan", "warning": "yellow", "error": "red",
		},
		"light": {
			"accent": "blue", "selected": "black", "muted": "bright_black",
			"border": "black", "user": "green", "assistant": "blue", "warning": "yellow", "error": "red",
		},
	}
	if color := palettes[m.cfg.UI.Theme][name]; color != "" {
		return color
	}
	return "default"
}

func applyForeground(style lipgloss.Style, color string) lipgloss.Style {
	if color == "" || color == "default" {
		return style
	}
	named := map[string]string{
		"black": "0", "red": "1", "green": "2", "yellow": "3",
		"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
		"bright_black": "8", "bright_red": "9", "bright_green": "10", "bright_yellow": "11",
		"bright_blue": "12", "bright_magenta": "13", "bright_cyan": "14", "bright_white": "15",
	}
	if ansi, ok := named[color]; ok {
		color = ansi
	}
	return style.Foreground(lipgloss.Color(color))
}

func wrapLines(text string, width int) []string {
	if text == "" {
		return nil
	}
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n") {
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		remaining := raw
		for lipgloss.Width(remaining) > width {
			head, tail := splitDisplayWidth(remaining, width)
			lines = append(lines, head)
			remaining = tail
		}
		lines = append(lines, remaining)
	}
	return lines
}

func truncate(value string, width int) string {
	value = strings.Join(strings.Fields(value), " ")
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	head, _ := splitDisplayWidth(value, width-1)
	return head + "…"
}

func splitDisplayWidth(value string, width int) (string, string) {
	if width <= 0 {
		return "", value
	}
	used, byteIndex := 0, 0
	for i, r := range value {
		runeWidth := lipgloss.Width(string(r))
		if used+runeWidth > width {
			return value[:i], value[i:]
		}
		used += runeWidth
		byteIndex = i + utf8.RuneLen(r)
	}
	return value[:byteIndex], value[byteIndex:]
}

func ConfigSummary(loaded config.Loaded, cachePath string) string {
	state := "defaults (file not found)"
	if loaded.Found {
		state = "loaded"
	}
	return fmt.Sprintf("config: %s [%s]\ncache:  %s\ncodex:  %s", filepath.Clean(loaded.Path), state, cachePath, loaded.Config.Codex.Binary)
}
