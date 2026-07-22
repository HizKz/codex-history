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

type model struct {
	ctx          context.Context
	cfg          config.Config
	configPath   string
	loadOptions  config.LoadOptions
	store        *index.Store
	client       *appserver.Client
	threads      []appserver.Thread
	visible      []appserver.Thread
	selected     int
	transcript   history.Transcript
	transcriptID string
	itemCursor   int
	expanded     map[string]bool
	query        string
	searching    bool
	focus        focus
	width        int
	height       int
	status       string
	err          string
	showHelp     bool
	loading      bool
	indexing     bool
	allSources   bool
	archived     bool
	printed      string
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
	query string
	ids   []string
	err   error
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
		expanded:    make(map[string]bool),
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
	return func() tea.Msg {
		results, err := m.store.Search(m.ctx, query, 500)
		ids := make([]string, 0, len(results))
		for _, result := range results {
			ids = append(ids, result.ID)
		}
		return searchMsg{query: query, ids: ids, err: err}
	}
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
		m.width, m.height = msg.Width, msg.Height
	case connectedMsg:
		m.loading = false
		if msg.err != nil {
			m.err, m.status = msg.err.Error(), "Unable to connect"
			return m, nil
		}
		m.client, m.threads, m.visible = msg.client, msg.threads, msg.threads
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
		m.threads, m.visible, m.query = msg.threads, msg.threads, ""
		m.status = fmt.Sprintf("%d conversations", len(m.threads))
		m.clampSelection()
		return m, m.loadSelectedCmd()
	case transcriptMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		if len(m.visible) > 0 && m.visible[m.selected].ID == msg.id {
			m.transcript, m.transcriptID, m.itemCursor = msg.transcript, msg.id, 0
		}
		if m.store != nil {
			_ = m.store.Upsert(m.ctx, msg.transcript.Thread, msg.transcript)
		}
	case searchMsg:
		if msg.query != m.query {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.applySearch(msg.ids)
		m.clampSelection()
		return m, m.loadSelectedCmd()
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
			return m, m.searchCmd()
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
	if m.searching {
		switch {
		case m.cfg.Keys.Match("search", "accept", key):
			m.searching, m.status = false, fmt.Sprintf("%d matches", len(m.visible))
			return m, nil
		case m.cfg.Keys.Match("search", "cancel", key):
			m.searching, m.query, m.visible = false, "", m.threads
			m.clampSelection()
			return m, m.loadSelectedCmd()
		case m.cfg.Keys.Match("search", "clear", key):
			m.query, m.visible = "", m.threads
			m.clampSelection()
			return m, m.loadSelectedCmd()
		case key == "backspace":
			if len(m.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(m.query)
				m.query = m.query[:len(m.query)-size]
			}
			if m.query == "" {
				m.visible = m.threads
				m.clampSelection()
				return m, m.loadSelectedCmd()
			}
			return m, m.searchCmd()
		default:
			if text := msg.Key().Text; text != "" {
				m.query += text
				return m, m.searchCmd()
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
	page := max(1, m.height-8)
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
		m.itemCursor = 0
		return m, m.loadSelectedCmd()
	}
	return m, nil
}

func (m model) handleTranscriptKey(key string) (tea.Model, tea.Cmd) {
	page := max(1, (m.height-8)/3)
	switch {
	case m.cfg.Keys.Match("transcript", "up", key):
		m.itemCursor--
	case m.cfg.Keys.Match("transcript", "down", key):
		m.itemCursor++
	case m.cfg.Keys.Match("transcript", "page_up", key):
		m.itemCursor -= page
	case m.cfg.Keys.Match("transcript", "page_down", key):
		m.itemCursor += page
	case m.cfg.Keys.Match("transcript", "toggle_item", key):
		if m.itemCursor >= 0 && m.itemCursor < len(m.transcript.Items) {
			item := m.transcript.Items[m.itemCursor]
			if item.Expandable {
				m.expanded[itemKey(item, m.itemCursor)] = !m.itemExpanded(item, m.itemCursor)
			}
		}
	}
	if len(m.transcript.Items) == 0 {
		m.itemCursor = 0
	} else {
		m.itemCursor = min(max(m.itemCursor, 0), len(m.transcript.Items)-1)
	}
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

func (m *model) applySearch(indexedIDs []string) {
	byID := make(map[string]appserver.Thread, len(m.threads))
	for _, thread := range m.threads {
		byID[thread.ID] = thread
	}
	seen := make(map[string]bool)
	visible := make([]appserver.Thread, 0, len(indexedIDs))
	for _, id := range indexedIDs {
		if thread, ok := byID[id]; ok {
			visible = append(visible, thread)
			seen[id] = true
		}
	}
	needle := strings.ToLower(strings.TrimSpace(m.query))
	for _, thread := range m.threads {
		metadata := history.Title(thread) + "\n" + thread.Preview + "\n" + thread.CWD
		if !seen[thread.ID] && strings.Contains(strings.ToLower(metadata), needle) {
			visible = append(visible, thread)
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
	mode := fmt.Sprintf("sources:%s  archived:%t", map[bool]string{true: "all", false: "config"}[m.allSources], m.archived)
	if m.indexing {
		mode += "  indexing…"
	}
	search := ""
	if m.searching || m.query != "" {
		search = "  / " + m.query
		if m.searching {
			search += "▌"
		}
	}
	line := title + "  " + m.mutedStyle().Render(mode) + search
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}

func (m model) renderList(width, height int) string {
	active := m.focus == focusList
	title := fmt.Sprintf(" Conversations (%d) ", len(m.visible))
	innerWidth := max(10, width-2)
	innerHeight := max(2, height-2)
	rows := max(1, innerHeight-1)
	start := 0
	if m.selected >= rows {
		start = m.selected - rows + 1
	}
	end := min(len(m.visible), start+rows)
	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		thread := m.visible[i]
		marker := "  "
		if thread.Archived {
			marker = "A "
		}
		when := ""
		reserved := 0
		if m.cfg.UI.ShowTimestamps {
			when = " " + time.Unix(thread.UpdatedAt, 0).Format(m.cfg.UI.DateFormat)
			reserved = utf8.RuneCountInString(when)
		}
		text := marker + truncate(history.Title(thread), max(8, innerWidth-4-reserved)) + when
		style := lipgloss.NewStyle().Width(innerWidth).MaxWidth(innerWidth)
		if i == m.selected {
			style = applyForeground(style, m.semanticColor("selected", m.cfg.UI.Colors.Selected)).Bold(true).Reverse(true)
		}
		lines = append(lines, style.Render(text))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + strings.Join(lines, "\n"))
}

func (m model) renderTranscript(width, height int) string {
	active := m.focus == focusTranscript
	innerWidth := max(10, width-2)
	innerHeight := max(2, height-2)
	rows := max(1, innerHeight-1)
	title := " Transcript "
	if m.transcriptID == "" {
		return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + m.mutedStyle().Render("Select a conversation"))
	}
	blocks := make([][]string, 0, len(m.transcript.Items))
	for i, item := range m.transcript.Items {
		marker := "  "
		if item.Expandable {
			if m.itemExpanded(item, i) {
				marker = "▼ "
			} else {
				marker = "▶ "
			}
		}
		heading := marker + item.Title
		if item.Status != "" {
			heading += "  [" + item.Status + "]"
		}
		headingStyle := lipgloss.NewStyle()
		switch item.Role {
		case "user":
			headingStyle = applyForeground(headingStyle, m.semanticColor("user", m.cfg.UI.Colors.User))
		case "assistant":
			headingStyle = applyForeground(headingStyle, m.semanticColor("assistant", m.cfg.UI.Colors.Assistant))
		}
		headingLines := wrapLines(heading, max(4, innerWidth-2))
		for i := range headingLines {
			headingLines[i] = headingStyle.Render(headingLines[i])
		}
		block := headingLines
		for _, line := range wrapLines(item.Text, max(4, innerWidth-4)) {
			block = append(block, "  "+line)
		}
		if m.itemExpanded(item, i) {
			for _, line := range wrapLines(item.Detail, max(4, innerWidth-4)) {
				block = append(block, "  "+line)
			}
		}
		if i == m.itemCursor {
			for j := range block {
				block[j] = lipgloss.NewStyle().Reverse(true).Width(innerWidth).MaxWidth(innerWidth).Render(block[j])
			}
		}
		blocks = append(blocks, block)
	}
	startItem := min(max(m.itemCursor, 0), max(0, len(blocks)-1))
	var lines []string
	for i := startItem; i < len(blocks) && len(lines) < rows; i++ {
		lines = append(lines, blocks[i]...)
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return m.panelStyle(active).Width(innerWidth).Height(innerHeight).Render(title + "\n" + strings.Join(lines, "\n"))
}

func (m model) renderHelp() string {
	var lines []string
	lines = append(lines, m.accentStyle().Bold(true).Render("codex-history key bindings"), "")
	for _, binding := range m.cfg.Keys.Bindings() {
		if len(binding.Keys) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%-28s %s", binding.Scope+"."+binding.Action, strings.Join(binding.Keys, ", ")))
	}
	lines = append(lines, "", "ctrl+c is always available as an emergency exit.", "Press ? to close help.")
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m model) footerText() string {
	if m.err != "" {
		return "error: " + m.err
	}
	if m.searching {
		return "type to search • enter accept • esc cancel • ctrl+u clear"
	}
	if m.cfg.UI.ShowHelp {
		return m.status + "  •  tab focus • / search • enter resume • ? help • q quit"
	}
	return m.status
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

func itemKey(item history.Item, index int) string {
	if item.ID != "" {
		return item.ID
	}
	return fmt.Sprintf("%s:%d", item.Kind, index)
}

func (m model) itemExpanded(item history.Item, i int) bool {
	if value, ok := m.expanded[itemKey(item, i)]; ok {
		return value
	}
	return m.cfg.UI.ToolDetails == "expanded"
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
