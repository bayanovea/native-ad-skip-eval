package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bayanovea/ad-skip-segment-eval/internal/score"
	"github.com/bayanovea/ad-skip-segment-eval/internal/store"
)

// runReview loads an eye-check sheet's verdicts (TSV) into gold_reviews.
//
//	adskip review -file <data>/review/2026-10-11-opus-v1.tsv -run 2026-10-11-claude-opus-5-5-v1 -reviewer user -date 2026-10-12
//
// TSV columns: provider, native_id, category, start, end, verdict, item, note.
func runReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	file := fs.String("file", "", "TSV with verdicts (required)")
	runID := fs.String("run", "", "run the sheet was built from (required)")
	reviewer := fs.String("reviewer", "user", "who made the verdicts")
	date := fs.String("date", "", "date of the sheet, YYYY-MM-DD (required)")
	data := fs.String("data", defaultDataDir(), "private data root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" || *runID == "" || *date == "" {
		return fmt.Errorf("-file, -run and -date are required")
	}
	f, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer f.Close()

	valid := map[string]bool{score.VerdictGoldMissing: true, score.VerdictGoldWrong: true, score.VerdictDefinitionUnclear: true, score.VerdictModelWrong: true, score.VerdictGoldValid: true}
	var rs []store.GoldReview
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		if line == 1 && strings.HasPrefix(sc.Text(), "provider\t") {
			continue
		}
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 7 {
			return fmt.Errorf("line %d: want 7–8 tab-separated columns, got %d", line, len(cols))
		}
		start, err1 := strconv.ParseFloat(cols[3], 64)
		end, err2 := strconv.ParseFloat(cols[4], 64)
		if err1 != nil || err2 != nil || end <= start {
			return fmt.Errorf("line %d: bad interval %q–%q", line, cols[3], cols[4])
		}
		if !valid[cols[5]] {
			return fmt.Errorf("line %d: unknown verdict %q", line, cols[5])
		}
		r := store.GoldReview{Provider: cols[0], NativeID: cols[1], Category: cols[2], Start: start, End: end, Verdict: cols[5], Item: cols[6],
			Reviewer: *reviewer, ReviewedAt: *date, SourceRun: *runID}
		if len(cols) > 7 {
			r.Note = cols[7]
		}
		rs = append(rs, r)
	}
	if err := sc.Err(); err != nil {
		return err
	}

	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.ImportReviews(context.Background(), rs)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, r := range rs {
		counts[r.Verdict]++
	}
	fmt.Printf("reviews: %d read, %d inserted (%d already stored); by verdict: %v\n", len(rs), n, len(rs)-n, counts)
	return nil
}
