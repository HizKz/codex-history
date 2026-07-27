package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/HizKz/codex-history/internal/history"
)

type transcriptView int

const (
	transcriptConversation transcriptView = iota
	transcriptActivity
	transcriptDetail
)

type transcriptLineRole int

const (
	lineBody transcriptLineRole = iota
	lineMuted
	lineUser
	lineAssistant
	lineActivity
)

type transcriptLineKind int

const (
	lineText transcriptLineKind = iota
	lineBlank
	lineTurnDivider
	lineBubbleTop
	lineBubbleBody
	lineBubbleBottom
)

type transcriptLineAlignment int

const (
	alignLeft transcriptLineAlignment = iota
	alignCenter
	alignRight
)

type transcriptLine struct {
	Text       string
	Role       transcriptLineRole
	Kind       transcriptLineKind
	Alignment  transcriptLineAlignment
	BlockWidth int
	Anchor     string
	Turn       int
	TurnStart  bool
	Activity   bool
	Actionable bool
}

func buildTranscriptLines(transcript history.Transcript, width int, showActivityEvents bool) []transcriptLine {
	width = max(4, width)
	var lines []transcriptLine
	spacingIndex := 0
	appendLine := func(line transcriptLine) {
		lines = append(lines, line)
	}
	appendSpacing := func(turn int) {
		if len(lines) > 0 && lines[len(lines)-1].Kind != lineBlank {
			appendLine(transcriptLine{
				Role:   lineMuted,
				Kind:   lineBlank,
				Anchor: fmt.Sprintf("turn:%d:spacing:%d", turn, spacingIndex),
				Turn:   turn,
			})
			spacingIndex++
		}
	}
	appendText := func(text string, role transcriptLineRole, alignment transcriptLineAlignment, anchor string, turn int) {
		wrapped := wrapLines(text, width)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, line := range wrapped {
			appendLine(transcriptLine{
				Text: line, Role: role, Kind: lineText, Alignment: alignment,
				Anchor: anchor, Turn: turn,
			})
		}
	}
	appendMessage := func(item history.Item, turn, itemIndex int) {
		role := lineBody
		alignment := alignLeft
		if item.Role == "user" {
			role = lineUser
			alignment = alignRight
		} else if item.Role == "assistant" {
			role = lineAssistant
		}
		maxBubbleWidth := min(width, max(12, width*3/4))
		maxTextWidth := max(1, maxBubbleWidth-4)
		wrapped := wrapLines(item.Text, maxTextWidth)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		contentWidth := 0
		for _, line := range wrapped {
			contentWidth = max(contentWidth, displayWidth(line))
		}
		bubbleWidth := min(maxBubbleWidth, max(displayWidth(item.Title)+4, contentWidth+4))
		bubbleWidth = max(4, bubbleWidth)
		anchor := fmt.Sprintf("turn:%d:message:%d:%s", turn, itemIndex, item.ID)

		appendSpacing(turn)
		appendLine(transcriptLine{
			Text: item.Title, Role: role, Kind: lineBubbleTop, Alignment: alignment,
			BlockWidth: bubbleWidth, Anchor: anchor, Turn: turn,
		})
		for _, line := range wrapped {
			appendLine(transcriptLine{
				Text: line, Role: role, Kind: lineBubbleBody, Alignment: alignment,
				BlockWidth: bubbleWidth, Anchor: anchor, Turn: turn,
			})
		}
		appendLine(transcriptLine{
			Role: role, Kind: lineBubbleBottom, Alignment: alignment,
			BlockWidth: bubbleWidth, Anchor: anchor, Turn: turn,
		})
	}

	for turnIndex, turn := range transcript.Turns {
		appendSpacing(turnIndex)
		heading := fmt.Sprintf("TURN %d", turnIndex+1)
		if turn.Status != "" {
			heading += " · " + turn.Status
		}
		appendLine(transcriptLine{
			Text: heading, Role: lineMuted, Kind: lineTurnDivider, Alignment: alignCenter,
			Anchor: fmt.Sprintf("turn:%d", turnIndex), Turn: turnIndex, TurnStart: true,
		})

		for itemIndex, item := range turn.Primary {
			if item.Role == "user" {
				appendMessage(item, turnIndex, itemIndex)
			}
		}
		if len(turn.Activity) > 0 {
			appendSpacing(turnIndex)
			appendLine(transcriptLine{
				Text:       activitySummary(turn.Activity),
				Role:       lineActivity,
				Kind:       lineText,
				Alignment:  alignCenter,
				Anchor:     fmt.Sprintf("turn:%d:activity", turnIndex),
				Turn:       turnIndex,
				Activity:   true,
				Actionable: true,
			})
			if showActivityEvents {
				for i, item := range turn.Activity {
					appendText(
						activityEventLabel(i, item),
						lineMuted,
						alignCenter,
						fmt.Sprintf("turn:%d:activity:%d", turnIndex, i),
						turnIndex,
					)
				}
			}
		}
		for itemIndex, item := range turn.Primary {
			if item.Role != "user" {
				appendMessage(item, turnIndex, itemIndex)
			}
		}
	}
	return lines
}

func displayWidth(value string) int {
	return lipgloss.Width(value)
}

func activitySummary(items []history.Item) string {
	counts := map[string]int{}
	for _, item := range items {
		switch item.Kind {
		case "commandExecution":
			counts["cmd"]++
		case "fileChange":
			counts["files"]++
		case "mcpToolCall", "dynamicToolCall":
			counts["tools"]++
		case "plan":
			counts["plan"]++
		case "reasoning":
			counts["reasoning"]++
		case "agentMessage":
			counts["updates"]++
		case "webSearch":
			counts["web"]++
		default:
			counts["other"]++
		}
	}
	parts := []string{fmt.Sprintf("▶ Activity  %d", len(items))}
	for _, key := range []string{"cmd", "files", "tools", "plan", "reasoning", "updates", "web", "other"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", key, counts[key]))
		}
	}
	return strings.Join(parts, " · ")
}

func activityEventLabel(index int, item history.Item) string {
	label := fmt.Sprintf("%02d  %s", index+1, item.Title)
	if item.Status != "" {
		label += "  [" + item.Status + "]"
	}
	return label
}

func detailLines(item history.Item, width int) []string {
	width = max(4, width)
	title := item.Title
	if item.Status != "" {
		title += "  [" + item.Status + "]"
	}
	lines := wrapLines(title, width)
	if item.Text != "" {
		lines = append(lines, "")
		lines = append(lines, wrapLines(item.Text, width)...)
	}
	if item.Detail != "" {
		lines = append(lines, "")
		lines = append(lines, wrapLines(item.Detail, width)...)
	}
	if len(lines) == 0 {
		return []string{"No additional details"}
	}
	return lines
}

func visibleStart(offset, cursor, rows, total int) int {
	rows = max(1, rows)
	total = max(0, total)
	if total <= rows {
		return 0
	}
	offset = min(max(offset, 0), total-rows)
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}
	return min(max(offset, 0), total-rows)
}
