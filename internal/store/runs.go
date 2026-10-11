package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Run is a row of the runs table.
type Run struct {
	ID, CreatedAt, Model, Effort, PromptVersion, PromptSHA, Evalset, ConfigJSON string
}

// Call is one model call for one video.
type Call struct {
	RunID, Provider, NativeID, Status, Error                                string
	InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens, LatencyMS int
	CostUSD                                                                 float64
}

// Prediction is one raw detected segment.
type Prediction struct {
	RunID, Provider, NativeID, Category string
	Start, End                          float64
	FirstCue, LastCue                   int
	Confidence, Quote                   string
	QuoteOK                             bool
}

// CreateRun inserts the run row. A run id is reused only with -force, which
// deletes the previous calls and predictions first; a run is never appended to.
func (s *Store) CreateRun(ctx context.Context, r Run, replace bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		for _, q := range []string{`DELETE FROM predictions WHERE run_id = ?`, `DELETE FROM calls WHERE run_id = ?`, `DELETE FROM runs WHERE id = ?`} {
			if _, err := tx.ExecContext(ctx, q, r.ID); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO runs (id, created_at, model, effort, prompt_version, prompt_sha, evalset, config_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.CreatedAt, r.Model, r.Effort, r.PromptVersion, r.PromptSHA, r.Evalset, r.ConfigJSON)
	if err != nil {
		return fmt.Errorf("run %s exists (use -force to replace): %w", r.ID, err)
	}
	return tx.Commit()
}

// SaveCall writes one call and its predictions atomically, so a crash
// mid-run leaves either both or neither. An earlier failed call for the same
// video is replaced, which is how a resumed run retries its errors.
func (s *Store) SaveCall(ctx context.Context, c Call, preds []Prediction) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM predictions WHERE run_id = ? AND provider = ? AND native_id = ?`, c.RunID, c.Provider, c.NativeID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO calls (run_id, provider, native_id, status, error, input_tokens, output_tokens,
		cache_read_tokens, cache_write_tokens, latency_ms, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.RunID, c.Provider, c.NativeID, c.Status, nullS(c.Error), c.InputTokens, c.OutputTokens,
		c.CacheReadTokens, c.CacheWriteTokens, c.LatencyMS, c.CostUSD)
	if err != nil {
		return fmt.Errorf("call %s: %w", c.NativeID, err)
	}
	for _, p := range preds {
		_, err := tx.ExecContext(ctx, `INSERT INTO predictions (run_id, provider, native_id, category, start, "end", first_cue, last_cue,
			confidence, quote, quote_ok) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.RunID, p.Provider, p.NativeID, p.Category, p.Start, p.End, p.FirstCue, p.LastCue, p.Confidence, p.Quote, p.QuoteOK)
		if err != nil {
			return fmt.Errorf("prediction %s: %w", p.NativeID, err)
		}
	}
	return tx.Commit()
}

// CallStatus returns the stored status of a call in a run, or "" when the
// video has not been called yet. A resumed run skips "ok" and "no_transcript"
// and retries the rest, so an interrupted run never repeats a paid call but
// does repeat a failed one.
func (s *Store) CallStatus(ctx context.Context, runID, provider, nativeID string) (string, error) {
	var status string
	err := s.DB.QueryRowContext(ctx, `SELECT status FROM calls WHERE run_id = ? AND provider = ? AND native_id = ?`, runID, provider, nativeID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return status, err
}
