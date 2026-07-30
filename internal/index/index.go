package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/history"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

const currentSchemaVersion = 2

type Result struct {
	ID        string
	Title     string
	Preview   string
	CWD       string
	Source    string
	UpdatedAt int64
	Archived  bool
	Match     MatchField
	Snippet   []SnippetSegment
}

type MatchField string

const (
	MatchTitle   MatchField = "title"
	MatchPreview MatchField = "preview"
	MatchCWD     MatchField = "cwd"
	MatchBody    MatchField = "message"
)

type SnippetSegment struct {
	Text    string
	Matched bool
}

func DefaultPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "codex-history", "index.db"), nil
}

func Open(path string, persistent bool) (*Store, error) {
	if !persistent {
		path = "file:codex-history?mode=memory&cache=shared"
	} else {
		if path == "" {
			var err error
			path, err = DefaultPath()
			if err != nil {
				return nil, err
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create cache directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, path: path}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if persistent {
		_ = os.Chmod(path, 0o600)
	}
	return store, nil
}

func (s *Store) migrate() error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS threads (
			id TEXT PRIMARY KEY,
			updated_at INTEGER NOT NULL,
			title TEXT NOT NULL,
			preview TEXT NOT NULL,
			cwd TEXT NOT NULL,
			source TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS thread_fts USING fts5(
			thread_id UNINDEXED, title, preview, cwd, body, tokenize='trigram'
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize search index: %w", err)
		}
	}
	var version int
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.Exec(
			`INSERT INTO meta(key, value) VALUES('schema_version', ?)`,
			currentSchemaVersion,
		)
		return err
	}
	if err != nil {
		return fmt.Errorf("read search index schema version: %w", err)
	}
	if version == currentSchemaVersion {
		return nil
	}
	if version != 1 {
		return fmt.Errorf("unsupported search index schema version %d", version)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate search index: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM thread_fts; DELETE FROM threads`); err != nil {
		return fmt.Errorf("clear stale search index: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE meta SET value = ? WHERE key = 'schema_version'`,
		currentSchemaVersion,
	); err != nil {
		return fmt.Errorf("update search index schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit search index migration: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string { return s.path }

func (s *Store) NeedsIndex(ctx context.Context, id string, updatedAt int64) (bool, error) {
	var existing int64
	err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM threads WHERE id = ?`, id).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return existing != updatedAt, nil
}

func (s *Store) Upsert(ctx context.Context, thread appserver.Thread, transcript history.Transcript) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	title := history.Title(thread)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO threads(id, updated_at, title, preview, cwd, source, archived)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at, title=excluded.title,
		preview=excluded.preview, cwd=excluded.cwd, source=excluded.source, archived=excluded.archived`,
		thread.ID, thread.UpdatedAt, title, thread.Preview, thread.CWD, thread.Source, thread.Archived); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_fts WHERE thread_id = ?`, thread.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO thread_fts(thread_id, title, preview, cwd, body) VALUES(?, ?, ?, ?, ?)`,
		thread.ID, title, thread.Preview, thread.CWD, transcript.Body); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(query) < 3 {
		return s.searchLike(ctx, query, limit)
	}
	return s.searchFTS(ctx, query, limit)
}

const (
	highlightStart = "\uE000"
	highlightEnd   = "\uE001"
)

func (s *Store) searchFTS(ctx context.Context, query string, limit int) ([]Result, error) {
	ftsQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.title, t.preview, t.cwd, t.source, t.updated_at, t.archived,
		       highlight(thread_fts, 1, ?, ?),
		       highlight(thread_fts, 2, ?, ?),
		       highlight(thread_fts, 3, ?, ?),
		       snippet(thread_fts, 4, ?, ?, ' … ', 32)
		FROM thread_fts JOIN threads t ON t.id = thread_fts.thread_id
		WHERE thread_fts MATCH ?
		ORDER BY bm25(thread_fts, 0.0, 8.0, 4.0, 2.0, 1.0), t.updated_at DESC
		LIMIT ?`,
		highlightStart, highlightEnd,
		highlightStart, highlightEnd,
		highlightStart, highlightEnd,
		highlightStart, highlightEnd,
		ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var result Result
		var title, preview, cwd, body string
		if err := rows.Scan(
			&result.ID, &result.Title, &result.Preview, &result.CWD, &result.Source,
			&result.UpdatedAt, &result.Archived, &title, &preview, &cwd, &body,
		); err != nil {
			return nil, err
		}
		result.Match, result.Snippet = highlightedSnippet(title, preview, cwd, body)
		results = append(results, result)
	}
	return results, rows.Err()
}

func (s *Store) searchLike(ctx context.Context, query string, limit int) ([]Result, error) {
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.title, t.preview, t.cwd, t.source, t.updated_at, t.archived, f.body
		FROM thread_fts f JOIN threads t ON t.id = f.thread_id
		WHERE f.title LIKE ? ESCAPE '\' OR f.preview LIKE ? ESCAPE '\'
		   OR f.cwd LIKE ? ESCAPE '\' OR f.body LIKE ? ESCAPE '\'
		ORDER BY CASE
			WHEN f.title LIKE ? ESCAPE '\' THEN 0
			WHEN f.preview LIKE ? ESCAPE '\' THEN 1
			WHEN f.cwd LIKE ? ESCAPE '\' THEN 2
			ELSE 3
		END, t.updated_at DESC
		LIMIT ?`,
		pattern, pattern, pattern, pattern,
		pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var result Result
		var body string
		if err := rows.Scan(
			&result.ID, &result.Title, &result.Preview, &result.CWD, &result.Source,
			&result.UpdatedAt, &result.Archived, &body,
		); err != nil {
			return nil, err
		}
		result.Match, result.Snippet = literalSnippet(query, result.Title, result.Preview, result.CWD, body)
		results = append(results, result)
	}
	return results, rows.Err()
}

func highlightedSnippet(title, preview, cwd, body string) (MatchField, []SnippetSegment) {
	for _, candidate := range []struct {
		field MatchField
		text  string
	}{
		{MatchTitle, title},
		{MatchPreview, preview},
		{MatchCWD, cwd},
		{MatchBody, body},
	} {
		segments, matched := parseHighlight(candidate.text)
		if matched {
			return candidate.field, segments
		}
	}
	return "", nil
}

func parseHighlight(value string) ([]SnippetSegment, bool) {
	var segments []SnippetSegment
	matched := false
	for value != "" {
		start := strings.Index(value, highlightStart)
		if start < 0 {
			segments = appendSegment(segments, value, false)
			break
		}
		segments = appendSegment(segments, value[:start], false)
		value = value[start+len(highlightStart):]
		end := strings.Index(value, highlightEnd)
		if end < 0 {
			segments = appendSegment(segments, value, false)
			break
		}
		segments = appendSegment(segments, value[:end], true)
		matched = true
		value = value[end+len(highlightEnd):]
	}
	return segments, matched
}

func literalSnippet(query, title, preview, cwd, body string) (MatchField, []SnippetSegment) {
	for _, candidate := range []struct {
		field MatchField
		text  string
	}{
		{MatchTitle, title},
		{MatchPreview, preview},
		{MatchCWD, cwd},
		{MatchBody, body},
	} {
		if segments, ok := literalSegments(candidate.text, query); ok {
			return candidate.field, segments
		}
	}
	return "", nil
}

func literalSegments(value, query string) ([]SnippetSegment, bool) {
	start := strings.Index(value, query)
	if start < 0 && isASCII(query) {
		start = strings.Index(strings.ToLower(value), strings.ToLower(query))
	}
	if start < 0 {
		return nil, false
	}
	end := start + len(query)
	const contextRunes = 40
	prefix := []rune(value[:start])
	match := value[start:end]
	suffix := []rune(value[end:])
	left := ""
	if len(prefix) > contextRunes {
		left = "…"
		prefix = prefix[len(prefix)-contextRunes:]
	}
	right := ""
	if len(suffix) > contextRunes {
		right = "…"
		suffix = suffix[:contextRunes]
	}
	return []SnippetSegment{
		{Text: left + string(prefix)},
		{Text: match, Matched: true},
		{Text: string(suffix) + right},
	}, true
}

func appendSegment(segments []SnippetSegment, text string, matched bool) []SnippetSegment {
	if text == "" {
		return segments
	}
	if len(segments) > 0 && segments[len(segments)-1].Matched == matched {
		segments[len(segments)-1].Text += text
		return segments
	}
	return append(segments, SnippetSegment{Text: text, Matched: matched})
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func (s *Store) Clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM thread_fts; DELETE FROM threads;`)
	return err
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM threads`).Scan(&count)
	return count, err
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}
