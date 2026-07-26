package index

import (
	"context"
	"strings"
	"testing"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/history"
)

func TestIndexLifecycle(t *testing.T) {
	store, err := Open("", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	thread := appserver.Thread{ID: "abc", Preview: "fallback", CWD: "/work/demo", Source: "cli", UpdatedAt: 42}
	transcript := history.Transcript{Thread: thread, Body: "日本語の会話 full text"}
	if err := store.Upsert(ctx, thread, transcript); err != nil {
		t.Fatal(err)
	}
	needed, err := store.NeedsIndex(ctx, thread.ID, thread.UpdatedAt)
	if err != nil || needed {
		t.Fatalf("NeedsIndex = %t, %v", needed, err)
	}
	results, err := store.Search(ctx, "日本語", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != thread.ID {
		t.Fatalf("unexpected results: %#v", results)
	}
	if results[0].Match != MatchBody || snippetText(results[0].Snippet) == "" || !hasMatchedSegment(results[0].Snippet, "日本語") {
		t.Fatalf("unexpected match context: %#v", results[0])
	}
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	count, err := store.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Count = %d, %v", count, err)
	}
}

func TestSearchTreatsWildcardsLiterally(t *testing.T) {
	store, err := Open("", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	thread := appserver.Thread{ID: "percent", Preview: "100% done", UpdatedAt: 1}
	if err := store.Upsert(ctx, thread, history.Transcript{Thread: thread}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(ctx, "%", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("Search = %#v, %v", results, err)
	}
}

func TestSearchRanksMetadataBeforeBodyAndBreaksTiesByRecency(t *testing.T) {
	store, err := Open("", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	title := "needle in title"
	threads := []struct {
		thread appserver.Thread
		body   string
	}{
		{appserver.Thread{ID: "body", Preview: "other", UpdatedAt: 30}, "needle in body"},
		{appserver.Thread{ID: "new-title", Name: &title, UpdatedAt: 20}, ""},
		{appserver.Thread{ID: "old-title", Name: &title, UpdatedAt: 10}, ""},
	}
	for _, entry := range threads {
		if err := store.Upsert(ctx, entry.thread, history.Transcript{Thread: entry.thread, Body: entry.body}); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.Search(ctx, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.ID)
	}
	if strings.Join(got, ",") != "new-title,old-title,body" {
		t.Fatalf("ranked IDs = %v", got)
	}
	if results[0].Match != MatchTitle || !hasMatchedSegment(results[0].Snippet, "needle") {
		t.Fatalf("title match = %#v", results[0])
	}
}

func TestSearchPreservesLiteralQueriesAndShortSubstrings(t *testing.T) {
	store, err := Open("", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	thread := appserver.Thread{
		ID:        "literal",
		Preview:   `日本語 100% value_name a\b a"b literal AND token`,
		UpdatedAt: 1,
	}
	if err := store.Upsert(ctx, thread, history.Transcript{Thread: thread}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"日本", "%", "_", `\`, `a"b`, "AND"} {
		t.Run(query, func(t *testing.T) {
			results, err := store.Search(ctx, query, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].ID != thread.ID {
				t.Fatalf("Search(%q) = %#v", query, results)
			}
			if snippetText(results[0].Snippet) == "" {
				t.Fatalf("Search(%q) returned no snippet: %#v", query, results[0])
			}
		})
	}
}

func snippetText(segments []SnippetSegment) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString(segment.Text)
	}
	return builder.String()
}

func hasMatchedSegment(segments []SnippetSegment, text string) bool {
	for _, segment := range segments {
		if segment.Matched && strings.Contains(segment.Text, text) {
			return true
		}
	}
	return false
}
