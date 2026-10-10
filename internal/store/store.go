// Package store is the offline eval store: an SQLite file in the private data
// root holding gold labels and, later, runs and predictions. Only the CLI and
// the scorer depend on it. The detector must never import this package: in the
// product the same detector runs against the product's own database.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Store wraps the SQLite connection. Plain database/sql so a move to Postgres
// would be a driver swap, not a rewrite.
type Store struct {
	DB *sql.DB
}

// Path is the store file under a data root.
func Path(dataDir string) string { return filepath.Join(dataDir, "adskip.db") }

// Open opens (creating if needed) the store and applies the schema.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{DB: db}, nil
}

// Close closes the connection.
func (s *Store) Close() error { return s.DB.Close() }

// Video is a row of the videos table.
type Video struct {
	Provider, NativeID string
	Duration           *float64
}

// Contributor is a row of the contributors table.
type Contributor struct {
	ID         string
	IsAssessor bool
	Rating     *int
	Banned     bool
}

// Label is one human interval on one video, as a human made it.
type Label struct {
	Provider, NativeID string
	Contributor        string
	Category           string
	Start, End         float64
	CreatedAt          string // RFC 3339 UTC or ""
	Source             string // "markup" by default
}

// ImportStats is what one Import call did.
type ImportStats struct {
	Videos, Contributors, Labels, Duplicates int
}

// Import writes videos, contributors and labels in one transaction. Videos and
// contributors are upserted; a label identical to a stored one is skipped, so
// running the same file twice changes nothing.
func (s *Store) Import(ctx context.Context, videos []Video, contributors []Contributor, labels []Label) (ImportStats, error) {
	var st ImportStats
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return st, err
	}
	defer tx.Rollback()

	upVideo, err := tx.PrepareContext(ctx, `INSERT INTO videos (provider, native_id, duration) VALUES (?, ?, ?)
		ON CONFLICT (provider, native_id) DO UPDATE SET duration = COALESCE(excluded.duration, videos.duration)`)
	if err != nil {
		return st, err
	}
	defer upVideo.Close()
	for _, v := range videos {
		if _, err := upVideo.ExecContext(ctx, v.Provider, v.NativeID, nullF(v.Duration)); err != nil {
			return st, fmt.Errorf("video %s: %w", v.NativeID, err)
		}
		st.Videos++
	}

	upContrib, err := tx.PrepareContext(ctx, `INSERT INTO contributors (id, is_assessor, rating, banned) VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET is_assessor = excluded.is_assessor, rating = excluded.rating, banned = excluded.banned`)
	if err != nil {
		return st, err
	}
	defer upContrib.Close()
	for _, c := range contributors {
		if _, err := upContrib.ExecContext(ctx, c.ID, c.IsAssessor, nullI(c.Rating), c.Banned); err != nil {
			return st, fmt.Errorf("contributor %s: %w", c.ID, err)
		}
		st.Contributors++
	}

	insLabel, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO labels
		(provider, native_id, contributor, category, start, "end", created_at, source) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return st, err
	}
	defer insLabel.Close()
	for _, l := range labels {
		src := l.Source
		if src == "" {
			src = "markup"
		}
		res, err := insLabel.ExecContext(ctx, l.Provider, l.NativeID, l.Contributor, l.Category, l.Start, l.End, nullS(l.CreatedAt), src)
		if err != nil {
			return st, fmt.Errorf("label %s %s [%g,%g]: %w", l.NativeID, l.Category, l.Start, l.End, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			st.Duplicates++
		} else {
			st.Labels++
		}
	}
	return st, tx.Commit()
}

// Counts returns the row counts of the three tables, for summaries and checks.
func (s *Store) Counts(ctx context.Context) (videos, contributors, labels int, err error) {
	row := s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM videos), (SELECT count(*) FROM contributors), (SELECT count(*) FROM labels)`)
	err = row.Scan(&videos, &contributors, &labels)
	return
}

func nullF(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullI(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullS(s string) any {
	if s == "" {
		return nil
	}
	return s
}
