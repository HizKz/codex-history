package tui

import (
	"fmt"
	"strings"

	"github.com/HizKz/codex-history/internal/history"
)

type diffLineKind int

const (
	diffContext diffLineKind = iota
	diffAdded
	diffRemoved
	diffHunk
	diffMetadata
	diffFileHeader
)

type diffLine struct {
	Text string
	Kind diffLineKind
}

func buildTurnDiffLines(transcript history.Transcript, turnIndex int) []diffLine {
	if turnIndex < 0 || turnIndex >= len(transcript.Turns) {
		return nil
	}
	var lines []diffLine
	for _, item := range transcript.Turns[turnIndex].Activity {
		if item.Kind != "fileChange" {
			continue
		}
		for _, change := range item.FileChanges {
			if len(lines) > 0 {
				lines = append(lines, diffLine{})
			}
			header := strings.TrimSpace(change.Kind + " · " + change.Path)
			lines = append(lines, diffLine{Text: header, Kind: diffFileHeader})
			diff := strings.TrimRight(strings.ReplaceAll(change.Diff, "\r\n", "\n"), "\n")
			if diff == "" {
				lines = append(lines, diffLine{Text: "(No diff body)", Kind: diffMetadata})
				continue
			}
			for _, raw := range strings.Split(diff, "\n") {
				lines = append(lines, diffLine{
					Text: strings.ReplaceAll(raw, "\t", "    "),
					Kind: classifyDiffLine(raw),
				})
			}
		}
	}
	return lines
}

func classifyDiffLine(line string) diffLineKind {
	switch {
	case strings.HasPrefix(line, "@@"):
		return diffHunk
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, `\ No newline`):
		return diffMetadata
	case strings.HasPrefix(line, "+"):
		return diffAdded
	case strings.HasPrefix(line, "-"):
		return diffRemoved
	default:
		return diffContext
	}
}

func turnDiffFileCount(transcript history.Transcript, turnIndex int) int {
	if turnIndex < 0 || turnIndex >= len(transcript.Turns) {
		return 0
	}
	count := 0
	for _, item := range transcript.Turns[turnIndex].Activity {
		count += len(item.FileChanges)
	}
	return count
}

func emptyTurnDiffMessage(turnIndex int) string {
	return fmt.Sprintf("No file changes in Turn %d", turnIndex+1)
}

func sliceDisplayColumns(value string, offset, width int) string {
	if width <= 0 || value == "" {
		return ""
	}
	offset = max(0, offset)
	_, tail := splitDisplayWidth(value, offset)
	head, _ := splitDisplayWidth(tail, width)
	return head
}
