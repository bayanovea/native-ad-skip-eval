// Package gold reads the anonymised gold export (one JSON object per line, as
// written by the product's scripts/export-gold.sql) and turns it into store
// rows. Validation collects every problem instead of stopping at the first:
// a 12k-line export with three bad rows should import 12k-3 rows and say which.
package gold

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/bayanovea/ad-skip-segment-eval/internal/store"
)

// Categories is the closed set of label categories in the product.
var Categories = map[string]bool{"sponsor": true, "intro": true, "self_promotion": true, "credits": true}

// Row is one line of the export file.
type Row struct {
	Provider          string   `json:"provider"`
	NativeID          string   `json:"native_id"`
	Duration          *float64 `json:"duration"`
	Category          string   `json:"category"`
	Start             float64  `json:"start"`
	End               float64  `json:"end"`
	Contributor       string   `json:"contributor"`
	IsAssessor        bool     `json:"is_assessor"`
	ContributorRating *int     `json:"contributor_rating"`
	ContributorBanned bool     `json:"contributor_banned"`
	CreatedAt         string   `json:"created_at"`
}

// Batch is a validated export, deduplicated into store rows.
type Batch struct {
	Videos       []store.Video
	Contributors []store.Contributor
	Labels       []store.Label
	Read         int      // lines read
	Rejected     []string // "line N: reason", one per bad line
}

// ReadFile parses and validates an export file.
func ReadFile(path string) (*Batch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read parses and validates an export stream.
func Read(r io.Reader) (*Batch, error) {
	b := &Batch{}
	videos := map[[2]string]store.Video{}
	contribs := map[string]store.Contributor{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		b.Read++
		var row Row
		if err := json.Unmarshal([]byte(text), &row); err != nil {
			b.Rejected = append(b.Rejected, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		if reason := validate(row); reason != "" {
			b.Rejected = append(b.Rejected, fmt.Sprintf("line %d: %s", line, reason))
			continue
		}
		key := [2]string{row.Provider, row.NativeID}
		if v, ok := videos[key]; !ok || (v.Duration == nil && row.Duration != nil) {
			videos[key] = store.Video{Provider: row.Provider, NativeID: row.NativeID, Duration: row.Duration}
		}
		contribs[row.Contributor] = store.Contributor{ID: row.Contributor, IsAssessor: row.IsAssessor,
			Rating: row.ContributorRating, Banned: row.ContributorBanned}
		b.Labels = append(b.Labels, store.Label{Provider: row.Provider, NativeID: row.NativeID,
			Contributor: row.Contributor, Category: row.Category, Start: row.Start, End: row.End,
			CreatedAt: row.CreatedAt, Source: "markup"})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, v := range videos {
		b.Videos = append(b.Videos, v)
	}
	sort.Slice(b.Videos, func(i, j int) bool { return b.Videos[i].NativeID < b.Videos[j].NativeID })
	for _, c := range contribs {
		b.Contributors = append(b.Contributors, c)
	}
	sort.Slice(b.Contributors, func(i, j int) bool { return b.Contributors[i].ID < b.Contributors[j].ID })
	return b, nil
}

func validate(r Row) string {
	switch {
	case r.Provider == "":
		return "empty provider"
	case r.NativeID == "":
		return "empty native_id"
	case r.Contributor == "":
		return "empty contributor"
	case !Categories[r.Category]:
		return "unknown category " + strconv(r.Category)
	case r.Start < 0:
		return fmt.Sprintf("negative start %g", r.Start)
	case r.End <= r.Start:
		return fmt.Sprintf("end %g <= start %g", r.End, r.Start)
	}
	return ""
}

func strconv(s string) string { return fmt.Sprintf("%q", s) }
