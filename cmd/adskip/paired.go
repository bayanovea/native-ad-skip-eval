package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bayanovea/native-ad-skip-eval/internal/score"
	"github.com/bayanovea/native-ad-skip-eval/internal/store"
)

// runPaired compares a candidate run with a base run on the videos both
// processed, under the headline settings, and gives the SCORE difference a
// bootstrap interval twice: resampling videos, then whole channels.
//
//	adskip paired [-gold reviewed] [-lambda 10] [-videos FILE] [-timing-clean] BASE_RUN CANDIDATE_RUN
func runPaired(args []string) error {
	fs := flag.NewFlagSet("paired", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	iou := fs.Float64("iou", 0.3, "minimum intersection-over-union for a hit")
	minSeconds := fs.Float64("min-seconds", 5, "drop predictions shorter than this")
	goldMode := fs.String("gold", "reviewed", "all | assessor | reviewed")
	videosFile := fs.String("videos", "", "score only the videos listed in this file (one native id per line)")
	timingClean := fs.Bool("timing-clean", false, "drop videos with an unreliable timeline (as in rank)")
	lambda := fs.Float64("lambda", 10, "a wrongly cut second counts this many times worse than a missed ad second")
	iters := fs.Int("boot", 10000, "bootstrap resamples")
	seed := fs.Uint64("seed", 1, "bootstrap seed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("give BASE_RUN CANDIDATE_RUN")
	}
	baseRun, candRun := fs.Arg(0), fs.Arg(1)
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

	p := scoreParams{Gold: *goldMode, Videos: only, TimingClean: *timingClean, DataRoot: *data,
		Opt: score.Options{MinSeconds: *minSeconds, IoU: *iou, MinConfidenceBy: productConfidence}}
	// A video that failed in one run would count as "found nothing" there.
	common := map[string]bool{}
	inBase := map[string]bool{}
	bv, err := selectVideos(ctx, st, baseRun, p)
	if err != nil {
		return err
	}
	for _, v := range bv {
		inBase[v[1]] = true
	}
	cv, err := selectVideos(ctx, st, candRun, p)
	if err != nil {
		return err
	}
	var keys []string
	for _, v := range cv {
		if inBase[v[1]] {
			common[v[1]] = true
			keys = append(keys, v[0]+":"+v[1])
		}
	}
	p.Videos = common

	base, _, _, err := computeScore(ctx, st, baseRun, p)
	if err != nil {
		return fmt.Errorf("%s: %w", baseRun, err)
	}
	cand, _, _, err := computeScore(ctx, st, candRun, p)
	if err != nil {
		return fmt.Errorf("%s: %w", candRun, err)
	}
	// A video with neither gold nor predictions is still a draw in the resample.
	for _, k := range keys {
		for _, m := range []map[string]score.Seconds{base.VideoSeconds, cand.VideoSeconds} {
			if _, ok := m[k]; !ok {
				m[k] = score.Seconds{}
			}
		}
	}
	video := func(v string) string { return v }
	channel := func(v string) string {
		_, id, _ := strings.Cut(v, ":")
		owner, _, _ := strings.Cut(id, "_")
		return owner
	}
	byVideo, err := score.PairedDiff(base.VideoSeconds, cand.VideoSeconds, video, *lambda, *iters, *seed)
	if err != nil {
		return err
	}
	byChannel, err := score.PairedDiff(base.VideoSeconds, cand.VideoSeconds, channel, *lambda, *iters, *seed)
	if err != nil {
		return err
	}
	fmt.Printf("base       %-48s SCORE %.3f\n", baseRun, byVideo.Base)
	fmt.Printf("candidate  %-48s SCORE %.3f\n", candRun, byVideo.Candidate)
	fmt.Printf("difference %+.3f on %d common videos; %d videos changed, the largest is %.0f%% of the difference\n",
		byVideo.Diff, len(common), byVideo.Changed, 100*byVideo.MaxShare)
	fmt.Printf("95%% CI, resampling videos:   [%+.3f, %+.3f]  (%d units)\n", byVideo.Lo, byVideo.Hi, byVideo.Units)
	fmt.Printf("95%% CI, resampling channels: [%+.3f, %+.3f]  (%d units)\n", byChannel.Lo, byChannel.Hi, byChannel.Units)
	fmt.Printf("\n%s gold, λ = %g, product thresholds, %d resamples (seed %d). Gate: candidate passes when the lower bound > −margin.\n",
		*goldMode, *lambda, *iters, *seed)
	return nil
}
