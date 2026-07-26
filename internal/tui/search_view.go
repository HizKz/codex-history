package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/history"
	"github.com/HizKz/codex-history/internal/index"
)

func metadataSearchResult(thread appserver.Thread, query string) index.Result {
	result := index.Result{ID: thread.ID}
	for _, candidate := range []struct {
		field index.MatchField
		text  string
	}{
		{index.MatchTitle, history.Title(thread)},
		{index.MatchPreview, thread.Preview},
		{index.MatchCWD, thread.CWD},
	} {
		if segments, ok := localLiteralSegments(candidate.text, query); ok {
			result.Match, result.Snippet = candidate.field, segments
			return result
		}
	}
	return result
}

func localLiteralSegments(value, query string) ([]index.SnippetSegment, bool) {
	query = strings.TrimSpace(query)
	start := strings.Index(value, query)
	if start < 0 && asciiString(query) {
		start = strings.Index(strings.ToLower(value), strings.ToLower(query))
	}
	if start < 0 || query == "" {
		return nil, false
	}
	end := start + len(query)
	if end > len(value) {
		return nil, false
	}
	const contextRunes = 40
	prefix, suffix := []rune(value[:start]), []rune(value[end:])
	left, right := "", ""
	if len(prefix) > contextRunes {
		left = "…"
		prefix = prefix[len(prefix)-contextRunes:]
	}
	if len(suffix) > contextRunes {
		right = "…"
		suffix = suffix[:contextRunes]
	}
	return []index.SnippetSegment{
		{Text: left + string(prefix)},
		{Text: value[start:end], Matched: true},
		{Text: string(suffix) + right},
	}, true
}

func asciiString(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func (m model) renderSearchContext(result index.Result, width int, selected bool) string {
	baseStyle := m.conversationListMetadataStyle(selected)
	matchStyle := m.accentStyle().Bold(true)
	prefix := string(result.Match)
	if prefix == "" {
		prefix = "match"
	}
	prefix += " · "
	if lipgloss.Width(prefix) >= width {
		return baseStyle.Render(truncate(prefix, width))
	}

	segments := compactSearchSegments(result.Snippet)
	available := width - lipgloss.Width(prefix)
	total := 0
	for _, segment := range segments {
		total += lipgloss.Width(segment.Text)
	}
	limit := available
	truncated := total > available
	if truncated && limit > 0 {
		limit--
	}

	var output strings.Builder
	output.WriteString(baseStyle.Render(prefix))
	remaining := limit
	for _, segment := range segments {
		if remaining <= 0 {
			break
		}
		head, _ := splitDisplayWidth(segment.Text, remaining)
		style := baseStyle
		if segment.Matched {
			style = matchStyle
		}
		output.WriteString(style.Render(head))
		remaining -= lipgloss.Width(head)
	}
	if truncated {
		output.WriteString(baseStyle.Render("…"))
	}
	return output.String()
}

func compactSearchSegments(segments []index.SnippetSegment) []index.SnippetSegment {
	var compacted []index.SnippetSegment
	for _, segment := range segments {
		text := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, segment.Text)
		if text == "" {
			continue
		}
		if len(compacted) > 0 && compacted[len(compacted)-1].Matched == segment.Matched {
			compacted[len(compacted)-1].Text += text
		} else {
			compacted = append(compacted, index.SnippetSegment{Text: text, Matched: segment.Matched})
		}
	}
	return compacted
}
