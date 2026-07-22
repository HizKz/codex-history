package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/history"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

type Result struct {
	ID        string
	Title     string
	Preview   string
	CWD       string
	Source    string
	UpdatedAt int64
	Archived  bool
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
		`INSERT INTO meta(key, value) VALUES('schema_version', '1')
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize search index: %w", err)
		}
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
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.title, t.preview, t.cwd, t.source, t.updated_at, t.archived
		FROM thread_fts f JOIN threads t ON t.id = f.thread_id
		WHERE f.title LIKE ? ESCAPE '\' OR f.preview LIKE ? ESCAPE '\'
		   OR f.cwd LIKE ? ESCAPE '\' OR f.body LIKE ? ESCAPE '\'
		ORDER BY t.updated_at DESC LIMIT ?`, pattern, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var result Result
		if err := rows.Scan(&result.ID, &result.Title, &result.Preview, &result.CWD, &result.Source, &result.UpdatedAt, &result.Archived); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
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
