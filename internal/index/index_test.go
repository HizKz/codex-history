package index

import (
	"context"
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
