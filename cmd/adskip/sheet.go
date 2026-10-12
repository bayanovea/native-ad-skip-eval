package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ad-skip-segment-eval/internal/provider"
	"ad-skip-segment-eval/internal/score"
	"ad-skip-segment-eval/internal/store"
)

// runSheet builds a blind review sheet from the union of several runs.
//
//	adskip sheet -runs RUN1,RUN2,... -out <data>/review/2026-10-12-blind.md
//
// An item is a stretch of a video that at least one run flagged and that the
// reviewed gold does not cover yet. Items are clustered across runs, shuffled
// by hash and shown without the model name or the number of runs that flagged
// them, so the reviewer judges the text, not the source. Who flagged what goes
// to <out>.key.tsv, a separate private file that is read after the verdicts.
//
// This is what removes the circularity of the first review sheet, which was
// built from one run's disagreements and so favoured that run.
func runSheet(args []string) error {
	fs := flag.NewFlagSet("sheet", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	providerName := fs.String("provider", "main", "video platform")
	runsFlag := fs.String("runs", "", "comma-separated run ids (required)")
	out := fs.String("out", "", "output .md path, outside the repo (required)")
	minSec := fs.Float64("min-seconds", 5, "ignore predictions shorter than this")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runsFlag == "" || *out == "" {
		return fmt.Errorf("-runs and -out are required")
	}
	runs := strings.Split(*runsFlag, ",")
	ctx := context.Background()
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()

	// Predictions per video, per run; only calls that were ok.
	type flag1 struct {
		run, cat string
		span
	}
	byVideo := map[string][]flag1{}
	for _, r := range runs {
		ok, err := okSet(ctx, st, r)
		if err != nil {
			return err
		}
		ps, err := st.Predictions(ctx, r)
		if err != nil {
			return err
		}
		for _, p := range ps {
			if ok[p.NativeID] && p.End-p.Start >= *minSec {
				byVideo[p.NativeID] = append(byVideo[p.NativeID], flag1{r, p.Category, span{p.Start, p.End}})
			}
		}
	}
	reviews, err := st.Reviews(ctx)
	if err != nil {
		return err
	}

	type item struct {
		video      string
		start, end float64
		cats       map[string]bool
		runs       map[string]bool
	}
	var items []item
	for id, fl := range byVideo {
		labels, err := st.Labels(ctx, *providerName, id)
		if err != nil {
			return err
		}
		var covered []span // reviewed gold (any category) plus every earlier verdict region
		var golds []score.Gold
		for _, l := range labels {
			golds = append(golds, score.Gold{Video: id, Category: l.Category, Start: l.Start, End: l.End})
		}
		var revs []score.Review
		for _, r := range reviews {
			if r.NativeID == id {
				revs = append(revs, score.Review{Video: id, Category: r.Category, Start: r.Start, End: r.End, Verdict: r.Verdict})
				covered = append(covered, span{r.Start, r.End})
			}
		}
		rg, _ := score.ApplyReviews(golds, nil, revs)
		for _, g := range rg {
			covered = append(covered, span{g.Start, g.End})
		}
		covered = mergeSpans(covered)

		// Keep flags that are less than half covered, then cluster what is left.
		var open []flag1
		for _, f := range fl {
			if inside(f.span, covered)/(f.e-f.s) < 0.5 {
				open = append(open, f)
			}
		}
		sort.Slice(open, func(i, j int) bool { return open[i].s < open[j].s })
		var cur *item
		for _, f := range open {
			if cur != nil && f.s <= cur.end {
				if f.e > cur.end {
					cur.end = f.e
				}
				cur.cats[f.cat], cur.runs[f.run] = true, true
				continue
			}
			if cur != nil {
				items = append(items, *cur)
			}
			cur = &item{video: id, start: f.s, end: f.e, cats: map[string]bool{f.cat: true}, runs: map[string]bool{f.run: true}}
		}
		if cur != nil {
			items = append(items, *cur)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return shuffleKey(items[i].video, items[i].start) < shuffleKey(items[j].video, items[j].start)
	})

	prov, err := provider.Lookup(*providerName)
	if err != nil {
		return err
	}
	var md, key strings.Builder
	fmt.Fprintf(&md, "# Blind review sheet (private, not in the repo)\n\nFor every item, write one verdict: **sponsor** / **self_promotion** / **not an ad** / **unclear** (+ one line why).\nThe stretch was flagged by at least one automatic detector run and is not covered by any human label or earlier verdict. The source is hidden on purpose. Times are player times.\n\n")
	key.WriteString("item\tnative_id\tstart\tend\tcategories\truns_flagging\n")
	for i, it := range items {
		n := fmt.Sprintf("C%d", i+1)
		cues, err := cueText(*data, *providerName, it.video, it.start, it.end)
		if err != nil {
			return err
		}
		fmt.Fprintf(&md, "### %s. %s · %s – %s (%.0f s)\n\nProposed as: %s\n\nLink: %s\n\n> %s\n\nVerdict: \n\n", n, it.video, clock(it.start), clock(it.end), it.end-it.start, joinKeys(it.cats, " / "), prov.LinkAt(it.video, int(it.start)-2), cues)
		fmt.Fprintf(&key, "%s\t%s\t%.1f\t%.1f\t%s\t%s\n", n, it.video, it.start, it.end, joinKeys(it.cats, "+"), joinKeys(it.runs, ","))
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(md.String()), 0o644); err != nil {
		return err
	}
	keyPath := strings.TrimSuffix(*out, ".md") + ".key.tsv"
	if err := os.WriteFile(keyPath, []byte(key.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("%d items from %d runs -> %s (key: %s)\n", len(items), len(runs), *out, keyPath)
	return nil
}

func inside(f span, cov []span) float64 {
	var t float64
	for _, c := range cov {
		if lo, hi := max(f.s, c.s), min(f.e, c.e); hi > lo {
			t += hi - lo
		}
	}
	return t
}

func shuffleKey(id string, start float64) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%.0f", id, start)))
	return hex.EncodeToString(h[:])
}

func joinKeys(m map[string]bool, sep string) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, sep)
}

// cueText returns the transcript text inside [start, end], capped for readability.
func cueText(data, provider, id string, start, end float64) (string, error) {
	_, cues, err := readTranscript(filepath.Join(cacheDir(data, provider), id+".jsonl"))
	if err != nil {
		return "", err
	}
	var parts []string
	for _, c := range cues {
		if c.End > start && c.Start < end {
			parts = append(parts, c.Text)
		}
	}
	t := strings.Join(parts, " ")
	if r := []rune(t); len(r) > 700 {
		t = string(r[:700]) + " […]"
	}
	return t, nil
}
