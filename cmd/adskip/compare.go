package main

import (
	"context"
	"flag"
	"fmt"
	"sort"

	"github.com/bayanovea/ad-skip-segment-eval/internal/score"
	"github.com/bayanovea/ad-skip-segment-eval/internal/store"
)

// runCompare measures how much two runs agree with each other, without gold.
//
//	adskip compare -a RUN -b RUN [-iou 0.5] [-min-seconds 5]
//
// This is the noise instrument: two runs of the same model and prompt differ
// by chance, and a difference between transports, prompts or formats means
// something only if it is larger than that. Only videos that are `ok` in both
// runs are compared. Reported per category and for `any_ad`: segments in each
// run, one-to-one matches at IoU >= -iou, the agreement F1 = 2M/(nA+nB), and
// the time overlap (seconds flagged by both / seconds flagged by either).
func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	a := fs.String("a", "", "run id A (required)")
	b := fs.String("b", "", "run id B (required)")
	iou := fs.Float64("iou", 0.5, "minimum IoU for a match")
	minSec := fs.Float64("min-seconds", 5, "drop predictions shorter than this")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *a == "" || *b == "" {
		return fmt.Errorf("-a and -b are required")
	}
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()

	okA, err := okSet(ctx, st, *a)
	if err != nil {
		return err
	}
	okB, err := okSet(ctx, st, *b)
	if err != nil {
		return err
	}
	common := map[string]bool{}
	for id := range okA {
		if okB[id] {
			common[id] = true
		}
	}
	pa, err := loadPreds(ctx, st, *a, common, *minSec)
	if err != nil {
		return err
	}
	pb, err := loadPreds(ctx, st, *b, common, *minSec)
	if err != nil {
		return err
	}
	fmt.Printf("A %s\nB %s\n%d videos ok in both (IoU >= %.2f, min %.0f s)\n\n", *a, *b, len(common), *iou, *minSec)
	fmt.Printf("%-15s %5s %5s %7s %9s %12s\n", "category", "nA", "nB", "match", "agree F1", "time overlap")
	for _, cat := range []string{"sponsor", "self_promotion", "any_ad"} {
		var nA, nB, m int
		var both, either float64
		for id := range common {
			x, y := filterCat(pa[id], cat), filterCat(pb[id], cat)
			nA += len(x)
			nB += len(y)
			m += greedyMatches(x, y, *iou)
			bo, ei := timeOverlap(x, y)
			both += bo
			either += ei
		}
		f1, ov := 0.0, 0.0
		if nA+nB > 0 {
			f1 = 2 * float64(m) / float64(nA+nB)
		}
		if either > 0 {
			ov = both / either
		}
		fmt.Printf("%-15s %5d %5d %7d %9.2f %12.2f\n", cat, nA, nB, m, f1, ov)
	}
	return nil
}

type span struct{ s, e float64 }

func okSet(ctx context.Context, st *store.Store, run string) (map[string]bool, error) {
	vs, err := st.RunVideos(ctx, run, "ok")
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, v := range vs {
		m[v[1]] = true
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("run %s has no ok videos (does it exist?)", run)
	}
	return m, nil
}

type catSpan struct {
	cat string
	span
}

func loadPreds(ctx context.Context, st *store.Store, run string, keep map[string]bool, minSec float64) (map[string][]catSpan, error) {
	ps, err := st.Predictions(ctx, run)
	if err != nil {
		return nil, err
	}
	out := map[string][]catSpan{}
	for _, p := range ps {
		if keep[p.NativeID] && p.End-p.Start >= minSec {
			out[p.NativeID] = append(out[p.NativeID], catSpan{p.Category, span{p.Start, p.End}})
		}
	}
	return out, nil
}

func filterCat(xs []catSpan, cat string) []span {
	var out []span
	for _, x := range xs {
		if cat == "any_ad" || x.cat == cat {
			out = append(out, x.span)
		}
	}
	return mergeSpans(out)
}

// mergeSpans unions overlapping spans (the any_ad view and a stable input for matching).
func mergeSpans(xs []span) []span {
	sort.Slice(xs, func(i, j int) bool { return xs[i].s < xs[j].s })
	var out []span
	for _, x := range xs {
		if n := len(out); n > 0 && x.s <= out[n-1].e {
			if x.e > out[n-1].e {
				out[n-1].e = x.e
			}
			continue
		}
		out = append(out, x)
	}
	return out
}

// greedyMatches counts one-to-one matches at IoU >= min, best pairs first.
func greedyMatches(a, b []span, min float64) int {
	type pair struct {
		i, j int
		v    float64
	}
	var ps []pair
	for i, x := range a {
		for j, y := range b {
			if v := score.IoU(x.s, x.e, y.s, y.e); v >= min {
				ps = append(ps, pair{i, j, v})
			}
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].v > ps[j].v })
	usedA, usedB := map[int]bool{}, map[int]bool{}
	n := 0
	for _, p := range ps {
		if !usedA[p.i] && !usedB[p.j] {
			usedA[p.i], usedB[p.j] = true, true
			n++
		}
	}
	return n
}

// timeOverlap returns seconds covered by both span lists and by either (inputs are merged).
func timeOverlap(a, b []span) (both, either float64) {
	for _, x := range a {
		either += x.e - x.s
	}
	for _, y := range b {
		either += y.e - y.s
	}
	for _, x := range a {
		for _, y := range b {
			lo, hi := max(x.s, y.s), min(x.e, y.e)
			if hi > lo {
				both += hi - lo
			}
		}
	}
	return both, either - both
}
