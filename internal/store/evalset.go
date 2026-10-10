package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sort"
)

// VideoStats is what eval-set selection needs to know about a video.
type VideoStats struct {
	Provider, NativeID string
	Assessor           bool // has at least one assessor label
	Contributors       int  // distinct contributors with live labels
}

// VideoStatsAll returns one row per video that has labels, sorted by id.
func (s *Store) VideoStatsAll(ctx context.Context, provider string) ([]VideoStats, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT l.native_id, MAX(c.is_assessor), COUNT(DISTINCT l.contributor)
		FROM labels l JOIN contributors c ON c.id = l.contributor
		WHERE l.provider = ?
		GROUP BY l.native_id ORDER BY l.native_id`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VideoStats
	for rows.Next() {
		v := VideoStats{Provider: provider}
		var assessor int
		if err := rows.Scan(&v.NativeID, &assessor, &v.Contributors); err != nil {
			return nil, err
		}
		v.Assessor = assessor == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

// Bucket maps a video id to a stable bucket in [0, n). Selection by bucket is
// reproducible from the data alone (no seed file) and stays stable when new
// videos arrive: an old video never changes bucket.
func Bucket(provider, nativeID string, n uint64) uint64 {
	h := sha256.Sum256([]byte(provider + ":" + nativeID))
	return binary.BigEndian.Uint64(h[:8]) % n
}

// SortByHash orders videos by their hash so "the first N" is deterministic
// and unrelated to id order (ids correlate with channel and upload time).
func SortByHash(vs []VideoStats) {
	sort.Slice(vs, func(i, j int) bool {
		hi := sha256.Sum256([]byte(vs[i].Provider + ":" + vs[i].NativeID))
		hj := sha256.Sum256([]byte(vs[j].Provider + ":" + vs[j].NativeID))
		return string(hi[:]) < string(hj[:])
	})
}
