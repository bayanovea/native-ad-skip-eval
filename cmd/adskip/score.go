package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ad-skip-segment-eval/internal/score"
	"ad-skip-segment-eval/internal/store"
)

// runScore compares a run's predictions with the gold labels of the videos
// the run actually processed (status ok) and writes aggregates to
// runs/<id>/score-<settings>.json. Per-video detail (false positives, misses)
// goes to stdout only when asked, never to a file in the repo.
//
//	adskip score -run 2026-10-11-claude-opus-5-5-v1 [-min-seconds 5] [-min-confidence low] [-iou 0.5] [-gold all|assessor] [-list]
func runScore(args []string) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	runID := fs.String("run", "", "run id (directory name under runs/, required)")
	data := fs.String("data", defaultDataDir(), "private data root")
	minSeconds := fs.Float64("min-seconds", 5, "drop predictions shorter than this")
	minConf := fs.String("min-confidence", "", "drop predictions below this confidence: low, medium or high ('' = keep all)")
	iou := fs.Float64("iou", 0.5, "minimum intersection-over-union for a hit")
	goldMode := fs.String("gold", "all", "which human labels count: all | assessor")
	list := fs.Bool("list", false, "print false positives and misses with video ids to stdout (private)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return fmt.Errorf("-run is required")
	}
	if *goldMode != "all" && *goldMode != "assessor" {
		return fmt.Errorf("-gold must be all or assessor")
	}
	runDir := filepath.Join("runs", *runID)
	if _, err := os.Stat(runDir); err != nil {
		return fmt.Errorf("run directory %s: %w", runDir, err)
	}

	ctx := context.Background()
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()

	videos, err := st.RunVideos(ctx, *runID, "ok")
	if err != nil {
		return err
	}
	if len(videos) == 0 {
		return fmt.Errorf("run %s has no ok videos in the store", *runID)
	}
	// In assessor mode a video without any assessor label has no gold at all,
	// so it is left out entirely rather than scored as "no ads here".
	okVideo := map[string]bool{}
	var golds []score.Gold
	scored := 0
	for _, v := range videos {
		key := v[0] + ":" + v[1]
		labels, err := st.Labels(ctx, v[0], v[1])
		if err != nil {
			return err
		}
		var vg []score.Gold
		hasAssessor := false
		for _, l := range labels {
			hasAssessor = hasAssessor || l.Assessor
			if *goldMode == "assessor" && !l.Assessor {
				continue
			}
			vg = append(vg, score.Gold{Video: key, Category: l.Category, Start: l.Start, End: l.End, Contributor: l.Contributor, Assessor: l.Assessor})
		}
		if *goldMode == "assessor" && !hasAssessor {
			continue
		}
		okVideo[key] = true
		scored++
		golds = append(golds, vg...)
	}
	rawPreds, err := st.Predictions(ctx, *runID)
	if err != nil {
		return err
	}
	var preds []score.Pred
	for _, p := range rawPreds {
		key := p.Provider + ":" + p.NativeID
		if !okVideo[key] {
			continue // predictions of a call that later failed validation are not scored
		}
		preds = append(preds, score.Pred{Video: key, Category: p.Category, Start: p.Start, End: p.End, Confidence: p.Confidence})
	}

	opt := score.Options{MinSeconds: *minSeconds, MinConfidence: *minConf, IoU: *iou}
	res := score.Score(golds, preds, opt)

	tag := fmt.Sprintf("s%g-c%s-iou%g-%s", *minSeconds, orDefault(*minConf, "any"), *iou, *goldMode)
	out := scoreFile{
		Run: *runID, Settings: scoreSettings{MinSeconds: *minSeconds, MinConfidence: orDefault(*minConf, "any"), IoU: *iou, Gold: *goldMode},
		VideosScored: scored, PredsTotal: res.PredsTotal, PredsScored: res.PredsScored,
		GoldMerged: res.GoldMerged, Categories: map[string]catMetrics{},
	}
	for cat, c := range res.ByCategory {
		m := catMetrics{TP: c.TP, FP: c.FP, FN: c.FN, Precision: round3(c.Precision()), Recall: round3(c.Recall()), F1: round3(c.F1()),
			ByConfidence: map[string]confMetrics{}, StartError: toBoundary(res.Start[cat]), EndError: toBoundary(res.End[cat])}
		for conf, cc := range res.ByConfidence[cat] {
			m.ByConfidence[conf] = confMetrics{TP: cc.TP, FP: cc.FP, Precision: round3(cc.Precision())}
		}
		out.Categories[cat] = m
	}
	path := filepath.Join(runDir, "score-"+tag+".json")
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return err
	}

	printScore(out)
	fmt.Printf("\nwritten %s\n", path)
	if *list {
		printList(res)
	}
	return nil
}

type scoreSettings struct {
	MinSeconds    float64 `json:"min_seconds"`
	MinConfidence string  `json:"min_confidence"`
	IoU           float64 `json:"iou"`
	Gold          string  `json:"gold"`
}

type boundaryMetrics struct {
	N            int                `json:"n"`
	MedianSigned float64            `json:"median_signed_s"` // prediction minus gold; negative = prediction earlier
	AbsP50       float64            `json:"abs_p50_s"`
	AbsP90       float64            `json:"abs_p90_s"`
	Within       map[string]float64 `json:"within_tolerance"` // "2s" -> share of matched pairs
}

type confMetrics struct {
	TP, FP    int
	Precision float64 `json:"precision"`
}

type catMetrics struct {
	TP, FP, FN   int
	Precision    float64                `json:"precision"`
	Recall       float64                `json:"recall"`
	F1           float64                `json:"f1"`
	ByConfidence map[string]confMetrics `json:"by_confidence"`
	StartError   boundaryMetrics        `json:"start_error"`
	EndError     boundaryMetrics        `json:"end_error"`
}

type scoreFile struct {
	Run          string                `json:"run"`
	Settings     scoreSettings         `json:"settings"`
	VideosScored int                   `json:"videos_scored"`
	PredsTotal   int                   `json:"predictions_total"`
	PredsScored  int                   `json:"predictions_scored"`
	GoldMerged   map[string]int        `json:"gold_segments_merged"`
	Categories   map[string]catMetrics `json:"categories"`
}

func toBoundary(q score.Quantiles) boundaryMetrics {
	b := boundaryMetrics{N: q.N, MedianSigned: round1(q.MedianSigned), AbsP50: round1(q.AbsP50), AbsP90: round1(q.AbsP90), Within: map[string]float64{}}
	for tol, share := range q.WithinTolerance {
		b.Within[fmt.Sprintf("%gs", tol)] = round3(share)
	}
	return b
}

func printScore(f scoreFile) {
	fmt.Printf("run %s · %d videos · %d of %d predictions scored (min %gs, confidence ≥ %s, IoU ≥ %g, gold: %s)\n",
		f.Run, f.VideosScored, f.PredsScored, f.PredsTotal, f.Settings.MinSeconds, f.Settings.MinConfidence, f.Settings.IoU, f.Settings.Gold)
	fmt.Printf("%-15s %4s %4s %4s  %6s %6s %6s   %-26s %-26s\n", "category", "TP", "FP", "FN", "prec", "rec", "F1", "start err (med / p50 / p90)", "end err (med / p50 / p90)")
	cats := make([]string, 0, len(f.Categories))
	for c := range f.Categories {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, c := range cats {
		m := f.Categories[c]
		fmt.Printf("%-15s %4d %4d %4d  %6.2f %6.2f %6.2f   %-26s %-26s\n", c, m.TP, m.FP, m.FN, m.Precision, m.Recall, m.F1,
			fmt.Sprintf("%+.1f / %.1f / %.1f s", m.StartError.MedianSigned, m.StartError.AbsP50, m.StartError.AbsP90),
			fmt.Sprintf("%+.1f / %.1f / %.1f s", m.EndError.MedianSigned, m.EndError.AbsP50, m.EndError.AbsP90))
	}
	fmt.Println("precision by confidence:")
	for _, c := range cats {
		var parts []string
		for _, conf := range []string{"high", "medium", "low"} {
			if cc, ok := f.Categories[c].ByConfidence[conf]; ok {
				parts = append(parts, fmt.Sprintf("%s %.2f (%d/%d)", conf, cc.Precision, cc.TP, cc.TP+cc.FP))
			}
		}
		fmt.Printf("  %-15s %s\n", c, strings.Join(parts, " · "))
	}
}

func printList(res score.Result) {
	fmt.Println("\nfalse positives (no gold of the same category with IoU above threshold):")
	for _, p := range res.Unmatched {
		fmt.Printf("  %s  %-15s %s – %s  conf %s\n", p.Video, p.Category, clock(p.Start), clock(p.End), p.Confidence)
	}
	fmt.Println("misses (merged gold with no prediction of the same category above threshold):")
	for _, g := range res.Missed {
		fmt.Printf("  %s  %-15s %s – %s\n", g.Video, g.Category, clock(g.Start), clock(g.End))
	}
}

// clock renders seconds the way a video player shows them: m:ss or h:mm:ss.
func clock(sec float64) string {
	s := int(sec + 0.5)
	h, m, ss := s/3600, s%3600/60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, ss)
	}
	return fmt.Sprintf("%d:%02d", m, ss)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func round1(x float64) float64 { return float64(int(x*10+copysign(0.5, x))) / 10 }
func round3(x float64) float64 { return float64(int(x*1000+copysign(0.5, x))) / 1000 }

func copysign(mag, sign float64) float64 {
	if sign < 0 {
		return -mag
	}
	return mag
}
