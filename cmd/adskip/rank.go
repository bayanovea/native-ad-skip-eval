package main

import (
	"context"
	"flag"
	"fmt"
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
		res, _, _, err := computeScore(ctx, st, run, scoreParams{Gold: *goldMode, ExcludeOwner: exclude, Opt: opt})
		if err != nil {
			return 0, fmt.Errorf("%s: %w", run, err)
		}
		return score.NetScore(res.BySeconds[score.AnyAd], *lambda), nil
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

	header := fmt.Sprintf("%-28s %-26s", "variant", "SCORE all videos")
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
		line := fmt.Sprintf("%-28s %-26s", label, all)
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
