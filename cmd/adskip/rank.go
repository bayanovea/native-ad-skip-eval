package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"ad-skip-segment-eval/internal/score"
	"ad-skip-segment-eval/internal/store"
)

// runRank prints one SCORE per variant, so variants can be compared at a glance.
//
//	adskip rank [-exclude-owner c1] [-lambda 10] "v1.1=runA,runB" "v2.1=runC,runD"
//
// Each argument is label=run[,run...]; several runs of the same configuration
// give the mean and the min–max, which is the noise to read differences against.
// The score is the headline of `adskip score` (product thresholds, reviewed gold).
func runRank(args []string) error {
	fs := flag.NewFlagSet("rank", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	iou := fs.Float64("iou", 0.3, "minimum intersection-over-union for a hit")
	minSeconds := fs.Float64("min-seconds", 5, "drop predictions shorter than this")
	goldMode := fs.String("gold", "reviewed", "all | assessor | reviewed")
	excludeOwner := fs.String("exclude-owner", "", "second column: the same without videos of this owner part")
	timingClean := fs.Bool("timing-clean", false, "drop videos whose subtitle track ends >3 s after the video (unreliable timeline)")
	lambda := fs.Float64("lambda", 10, "a wrongly cut second counts this many times worse than a missed ad second")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("give at least one label=run[,run...]")
	}
	ctx := context.Background()
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()

	opt := score.Options{MinSeconds: *minSeconds, IoU: *iou, MinConfidenceBy: productConfidence}
	one := func(run, exclude string) (float64, error) {
		res, _, _, err := computeScore(ctx, st, run, scoreParams{Gold: *goldMode, ExcludeOwner: exclude, TimingClean: *timingClean, DataRoot: *data, Opt: opt})
		if err != nil {
			return 0, fmt.Errorf("%s: %w", run, err)
		}
		return score.NetScore(res.BySeconds[score.AnyAd], *lambda), nil
	}
	// raw returns, for the first run of a variant, the two numbers the headline
	// is made of: share of ad time removed, and seconds of content cut wrongly
	// per hour of video (duration from the transcript meta line).
	raw := func(run string) (string, error) {
		res, _, _, err := computeScore(ctx, st, run, scoreParams{Gold: *goldMode, TimingClean: *timingClean, DataRoot: *data, Opt: opt})
		if err != nil {
			return "", err
		}
		videos, err := st.RunVideos(ctx, run, "ok")
		if err != nil {
			return "", err
		}
		hours := 0.0
		for _, v := range videos {
			hours += transcriptDuration(*data, v[0], v[1]) / 3600
		}
		x := res.BySeconds[score.AnyAd]
		if hours == 0 {
			return fmt.Sprintf("%.0f%%  n/a", 100*x.Recall()), nil
		}
		return fmt.Sprintf("%.0f%%  %.1f s/h", 100*x.Recall(), x.WronglySkipped()/hours), nil
	}
	summary := func(runs []string, exclude string) (string, error) {
		var vals []float64
		for _, r := range runs {
			v, err := one(r, exclude)
			if err != nil {
				return "", err
			}
			vals = append(vals, v)
		}
		for _, v := range vals {
			if math.IsNaN(v) {
				return "n/a (no gold)", nil
			}
		}
		lo, hi, sum := vals[0], vals[0], 0.0
		for _, v := range vals {
			sum += v
			lo, hi = min(lo, v), max(hi, v)
		}
		if len(vals) == 1 {
			return fmt.Sprintf("%.2f", vals[0]), nil
		}
		return fmt.Sprintf("%.2f  (%.2f–%.2f, n=%d)", sum/float64(len(vals)), lo, hi, len(vals)), nil
	}

	header := fmt.Sprintf("%-28s %-26s %-20s", "variant", "SCORE all videos", "ad removed, wrong cut")
	if *excludeOwner != "" {
		header += " SCORE without " + *excludeOwner
	}
	fmt.Println(header)
	for _, a := range fs.Args() {
		label, list, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("argument %q: want label=run[,run...]", a)
		}
		runs := strings.Split(list, ",")
		all, err := summary(runs, "")
		if err != nil {
			return err
		}
		rawText, err := raw(runs[0])
		if err != nil {
			return err
		}
		line := fmt.Sprintf("%-28s %-26s %-20s", label, all, rawText)
		if *excludeOwner != "" {
			ex, err := summary(runs, *excludeOwner)
			if err != nil {
				return err
			}
			line += " " + ex
		}
		fmt.Println(line)
	}
	fmt.Printf("\nSCORE = share of ad time removed minus %g× the share of content wrongly removed; product thresholds; %s gold; counted in seconds.\n", *lambda, *goldMode)
	return nil
}

// transcriptDuration reads the duration from the meta line of a cached
// transcript; 0 when the transcript or the field is missing.
func transcriptDuration(data, provider, id string) float64 {
	f, err := os.Open(filepath.Join(data, "transcripts", provider, id+".jsonl"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	if !sc.Scan() {
		return 0
	}
	var meta struct {
		Duration float64 `json:"duration"`
	}
	if json.Unmarshal(sc.Bytes(), &meta) != nil {
		return 0
	}
	return meta.Duration
}
