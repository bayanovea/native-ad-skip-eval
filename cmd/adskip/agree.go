package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/bayanovea/native-ad-skip-eval/internal/score"
	"github.com/bayanovea/native-ad-skip-eval/internal/store"
)

// runAgree measures the product's two-model rule: which of the first run's
// served segments a second run confirms (same category, IoU at the threshold),
// and on the gold what the veto costs (true ads vetoed) and saves (wrong cuts
// vetoed), at the product threshold and two neighbours for comparison.
//
//	adskip agree [-gold reviewed] [-videos FILE] FIRST_RUN SECOND_RUN
func runAgree(args []string) error {
	fs := flag.NewFlagSet("agree", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	goldMode := fs.String("gold", "reviewed", "all | assessor | reviewed")
	goldIoU := fs.Float64("gold-iou", 0.3, "a served segment is a true ad when it overlaps gold by this much")
	minSeconds := fs.Float64("min-seconds", 5, "the product's AI_MIN_SEGMENT_SECONDS")
	videosFile := fs.String("videos", "", "only the videos listed in this file (one native id per line)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("give FIRST_RUN SECOND_RUN")
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

	p := scoreParams{Gold: *goldMode, Videos: only, DataRoot: *data}
	// Only videos both runs answered: a failed second call is no veto in the product.
	inSecond := map[string]bool{}
	sv, err := selectVideos(ctx, st, fs.Arg(1), p)
	if err != nil {
		return err
	}
	for _, v := range sv {
		inSecond[v[1]] = true
	}
	fv, err := selectVideos(ctx, st, fs.Arg(0), p)
	if err != nil {
		return err
	}
	common := map[string]bool{}
	for _, v := range fv {
		if inSecond[v[1]] {
			common[v[1]] = true
		}
	}
	p.Videos = common
	golds, first, _, _, err := scoredInputs(ctx, st, fs.Arg(0), p)
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	_, second, _, _, err := scoredInputs(ctx, st, fs.Arg(1), p)
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(1), err)
	}
	fmt.Printf("%d common videos, %s gold, served = product thresholds, min %g s, gold IoU %g\n\n", len(common), *goldMode, *minSeconds, *goldIoU)
	fmt.Printf("%-5s %7s %10s %12s %10s %10s %11s\n", "IoU", "served", "confirmed", "unconfirmed", "TP vetoed", "FP vetoed", "second-only")
	for _, iou := range []float64{0.3, 0.5, 0.7} {
		a := score.Agree(golds, first, second, score.AgreeOptions{
			Served: score.Options{MinSeconds: *minSeconds, MinConfidenceBy: productConfidence}, IoU: iou, GoldIoU: *goldIoU})
		fmt.Printf("%-5g %7d %10d %12d %4d/%-5d %4d/%-5d %11d\n", iou, a.Served, a.Confirmed, a.Unconfirmed,
			a.TPVetoed, a.TPVetoed+a.TPConfirmed, a.FPVetoed, a.FPVetoed+a.FPConfirmed, a.SecondOnly)
	}
	fmt.Println("\nTP vetoed = true ads the veto sends to review (recall cost); FP vetoed = wrong cuts it keeps from viewers.")
	return nil
}
