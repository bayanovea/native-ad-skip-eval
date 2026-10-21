package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
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
	goldMode := fs.String("gold", "all", "which human labels count: all | assessor | reviewed (all + eye-check verdicts applied)")
	noOffsets := fs.Bool("no-offsets", false, "ignore per-video timeline offsets (video_offsets)")
	excludeOwner := fs.String("exclude-owner", "", "leave out videos whose native id starts with this owner part (e.g. c1: a channel that dominates the set)")
	videosFile := fs.String("videos", "", "score only the videos listed in this file (one native id per line); its base name goes into the output file name")
	lambda := fs.Float64("lambda", 10, "headline: a wrongly cut second counts this many times worse than a missed ad second")
	detail := fs.Bool("detail", false, "also print the full table (segments, categories, boundaries, confidence levels)")
	list := fs.Bool("list", false, "print false positives and misses with video ids to stdout (private)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return fmt.Errorf("-run is required")
	}
	if *goldMode != "all" && *goldMode != "assessor" && *goldMode != "reviewed" {
		return fmt.Errorf("-gold must be all, assessor or reviewed")
	}
	runDir := filepath.Join("runs", *runID)
	if _, err := os.Stat(runDir); err != nil {
		return fmt.Errorf("run directory %s: %w", runDir, err)
	}

	only, err := readVideoSet(*videosFile)
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()

	opt := score.Options{MinSeconds: *minSeconds, MinConfidence: *minConf, IoU: *iou}
	res, scored, reviewsApplied, err := computeScore(ctx, st, *runID, scoreParams{Gold: *goldMode, ExcludeOwner: *excludeOwner, Videos: only, NoOffsets: *noOffsets, Opt: opt})
	if err != nil {
		return err
	}
	headline := func() error {
		hopt := score.Options{MinSeconds: *minSeconds, IoU: *iou, MinConfidenceBy: productConfidence}
		hres, _, _, err := computeScore(ctx, st, *runID, scoreParams{Gold: *goldMode, ExcludeOwner: *excludeOwner, Videos: only, NoOffsets: *noOffsets, Opt: hopt})
		if err != nil {
			return err
		}
		printHeadline(*runID, scored, *goldMode, hres.BySeconds[score.AnyAd], *lambda)
		return nil
	}

	tag := fmt.Sprintf("s%g-c%s-iou%g-%s", *minSeconds, orDefault(*minConf, "any"), *iou, *goldMode)
	if *excludeOwner != "" {
		tag += "-exowner" // not the owner id itself: it identifies a channel of the private set
	}
	if *videosFile != "" {
		tag += "-v" + strings.TrimSuffix(filepath.Base(*videosFile), filepath.Ext(*videosFile))
	}
	out := scoreFile{
		Run: *runID, Settings: scoreSettings{MinSeconds: *minSeconds, MinConfidence: orDefault(*minConf, "any"), IoU: *iou, Gold: *goldMode},
		VideosScored: scored, PredsTotal: res.PredsTotal, PredsScored: res.PredsScored, ReviewsApplied: reviewsApplied,
		GoldMerged: res.GoldMerged, Categories: map[string]catMetrics{},
	}
	for cat, c := range res.ByCategory {
		sec := res.BySeconds[cat]
		m := catMetrics{Seconds: secondsMetrics{Precision: round3(sec.Precision()), Recall: round3(sec.Recall()),
			WronglySkippedS: round1(sec.WronglySkipped()), MissedAdS: round1(sec.MissedAd()), PredictedS: round1(sec.Pred), GoldS: round1(sec.Gold)},
			TP: c.TP, FP: c.FP, FN: c.FN, Precision: round3(c.Precision()), Recall: round3(c.Recall()), F1: round3(c.F1()),
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

	if err := headline(); err != nil {
		return err
	}
	if *detail {
		fmt.Println()
		printScore(out)
		fmt.Printf("\nwritten %s\n", path)
	}
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

// secondsMetrics is the time-weighted view: share of skipped seconds that were
// ads (precision), share of ad seconds that were skipped (recall), and the two
// absolute error masses a viewer feels.
type secondsMetrics struct {
	Precision       float64 `json:"precision"`
	Recall          float64 `json:"recall"`
	WronglySkippedS float64 `json:"wrongly_skipped_s"`
	MissedAdS       float64 `json:"missed_ad_s"`
	PredictedS      float64 `json:"predicted_s"`
	GoldS           float64 `json:"gold_s"`
}

type catMetrics struct {
	Seconds      secondsMetrics `json:"seconds"`
	TP, FP, FN   int
	Precision    float64                `json:"precision"`
	Recall       float64                `json:"recall"`
	F1           float64                `json:"f1"`
	ByConfidence map[string]confMetrics `json:"by_confidence"`
	StartError   boundaryMetrics        `json:"start_error"`
	EndError     boundaryMetrics        `json:"end_error"`
}

type scoreFile struct {
	Run            string                `json:"run"`
	Settings       scoreSettings         `json:"settings"`
	VideosScored   int                   `json:"videos_scored"`
	PredsTotal     int                   `json:"predictions_total"`
	PredsScored    int                   `json:"predictions_scored"`
	ReviewsApplied int                   `json:"reviews_applied"`
	GoldMerged     map[string]int        `json:"gold_segments_merged"`
	Categories     map[string]catMetrics `json:"categories"`
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
	fmt.Println("seconds (time-weighted):   prec    rec   wrongly skipped / missed ad")
	for _, c := range cats {
		sm := f.Categories[c].Seconds
		fmt.Printf("  %-15s %6.2f %6.2f   %6.0f s / %6.0f s   (predicted %.0f s, gold %.0f s)\n", c, sm.Precision, sm.Recall, sm.WronglySkippedS, sm.MissedAdS, sm.PredictedS, sm.GoldS)
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

// printHeadline prints the one number used to compare variants, with the two
// quantities it is made of, in plain words. Product thresholds are applied
// (sponsor from medium, self_promotion only high). Ad time is the union of all
// categories.
func printHeadline(run string, videos int, gold string, x score.Seconds, lambda float64) {
	fmt.Printf("%s · %d videos · gold: %s\n", run, videos, gold)
	fmt.Printf("SCORE %s   (ad time removed, minus %g× the content wrongly removed; 1.0 = perfect, 0 = no detector)\n", fmtScore(score.NetScore(x, lambda)), lambda)
	fmt.Printf("  of %.0f s of ad: %.0f s removed (%.0f%%), %.0f s left in · content wrongly cut: %.0f s\n", x.Gold, x.Both, 100*x.Recall(), x.MissedAd(), x.WronglySkipped())
}

// fmtScore prints a SCORE, or "n/a" when the set has no gold ad time.
func fmtScore(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", v)
}
