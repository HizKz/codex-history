package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/HizKz/codex-history/internal/appserver"
)

type projectOption struct {
	CWD   string
	Label string
	Count int
}

func (m model) projectOptions() []projectOption {
	counts := make(map[string]int)
	for _, thread := range m.threads {
		if strings.TrimSpace(thread.CWD) != "" {
			counts[thread.CWD]++
		}
	}
	options := make([]projectOption, 0, len(counts)+1)
	options = append(options, projectOption{Label: "All projects", Count: len(m.threads)})
	for cwd, count := range counts {
		options = append(options, projectOption{
			CWD:   cwd,
			Label: filepath.Base(cwd) + " · " + cwd,
			Count: count,
		})
	}
	projects := options[1:]
	sort.Slice(projects, func(i, j int) bool {
		a, b := projects[i], projects[j]
		aBase, bBase := strings.ToLower(filepath.Base(a.CWD)), strings.ToLower(filepath.Base(b.CWD))
		if aBase != bBase {
			return aBase < bBase
		}
		return strings.ToLower(a.CWD) < strings.ToLower(b.CWD)
	})
	return options
}

func (m *model) openProjectPicker() {
	options := m.projectOptions()
	m.projectOpen = true
	m.projectCursor = 0
	for i, option := range options {
		if option.CWD == m.projectCWD {
			m.projectCursor = i
			break
		}
	}
}

func (m model) handleProjectKey(key string) (tea.Model, tea.Cmd) {
	options := m.projectOptions()
	switch {
	case m.cfg.Keys.Match("project", "cancel", key):
		m.projectOpen = false
		return m, nil
	case m.cfg.Keys.Match("project", "up", key):
		m.projectCursor--
	case m.cfg.Keys.Match("project", "down", key):
		m.projectCursor++
	case m.cfg.Keys.Match("project", "accept", key):
		if len(options) > 0 {
			m.projectCursor = min(max(m.projectCursor, 0), len(options)-1)
			m.projectCWD = options[m.projectCursor].CWD
		}
		m.projectOpen = false
		m.selected = 0
		m.applyCurrentFilters()
		m.clampSelection()
		m.status = fmt.Sprintf("%d conversations in %s", len(m.visible), m.projectLabel())
		return m, m.loadSelectedCmd()
	}
	if len(options) == 0 {
		m.projectCursor = 0
	} else {
		m.projectCursor = min(max(m.projectCursor, 0), len(options)-1)
	}
	return m, nil
}

func (m model) renderProjectPicker() string {
	width := max(m.width, 60)
	height := max(m.height, 16)
	innerWidth := max(10, width-2)
	innerHeight := max(5, height-4)
	rows := max(1, innerHeight-1)
	options := m.projectOptions()
	cursor := min(max(m.projectCursor, 0), max(0, len(options)-1))
	start := visibleStart(0, cursor, rows, len(options))
	end := min(len(options), start+rows)
	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		marker := "  "
		style := m.mutedStyle()
		if i == cursor {
			marker = "› "
			style = m.accentStyle().Bold(true)
		}
		label := fmt.Sprintf("%s  (%d)", options[i].Label, options[i].Count)
		lines = append(lines, lipgloss.NewStyle().Width(innerWidth).MaxWidth(innerWidth).
			Render(style.Render(marker+truncate(label, max(1, innerWidth-2)))))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	title := m.accentStyle().Bold(true).Render("codex-history")
	panelTitle := panelTitle("Select project", true, fmt.Sprintf("%d", len(options)))
	panel := m.panelStyle(true).Width(innerWidth).Height(innerHeight).
		Render(panelTitle + "\n" + strings.Join(lines, "\n"))
	footer := m.mutedStyle().MaxWidth(width).Render(strings.Join(compactHints(
		m.keyPairHint("project", "down", "up", "move"),
		m.keyHint("project", "accept", "select"),
		m.keyHint("project", "cancel", "cancel"),
	), " • "))
	return title + "\n" + panel + "\n" + footer
}

func (m model) matchesProject(thread appserver.Thread) bool {
	return m.projectCWD == "" || thread.CWD == m.projectCWD
}

func (m model) visibleForProject(threads []appserver.Thread) []appserver.Thread {
	if m.projectCWD == "" {
		return threads
	}
	visible := make([]appserver.Thread, 0, len(threads))
	for _, thread := range threads {
		if m.matchesProject(thread) {
			visible = append(visible, thread)
		}
	}
	return visible
}

func (m *model) clearMissingProject() bool {
	if m.projectCWD == "" {
		return false
	}
	for _, thread := range m.threads {
		if thread.CWD == m.projectCWD {
			return false
		}
	}
	m.projectCWD = ""
	return true
}

func (m model) projectLabel() string {
	if m.projectCWD == "" {
		return "all"
	}
	return filepath.Base(m.projectCWD)
}
