package main

import (
	"context"
	"flag"
	"fmt"
	"sort"

	"ad-skip-segment-eval/internal/score"
	"ad-skip-segment-eval/internal/store"
)

// runCeiling scores every human against the union of the other humans on the
// same video, with the same SCORE as a detector run. The result is the
// "human-human ceiling": what an annotator achieves when the reference is
// the other annotators, not an oracle.
//
//	adskip ceiling [-min-contributors 2] [-set NAME] [-lambda 10]
//
// Rules, all deliberate:
//   - only sponsor and self_promotion labels count; a contributor with none
//     of them on a video is left out (an intro-only marker did not say "no ads here");
//   - the predictor is one contributor, treated as "high" confidence (a human
//     label is a decision, there is no ranking signal);
//   - the reference is the union of the *other* contributors, the same union a
//     detector run is scored against, but without any eye-check verdicts: the
//     reviewed gold used for detector runs is cleaner, so a detector's SCORE
//     is slightly favoured in a comparison with this number.
func runCeiling(args []string) error {
	fs := flag.NewFlagSet("ceiling", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	provider := fs.String("provider", "main", "video platform")
	minContrib := fs.Int("min-contributors", 2, "videos with at least this many contributors (with sponsor/self_promotion labels)")
	setName := fs.String("set", "", "restrict to the videos of this eval set (default: every video in the store)")
	iou := fs.Float64("iou", 0.3, "unused by the seconds view; kept equal to the score command")
	minSeconds := fs.Float64("min-seconds", 5, "drop labels shorter than this on the predictor side")
	lambda := fs.Float64("lambda", 10, "a wrongly cut second counts this many times worse than a missed ad second")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()

	var ids []string
	if *setName != "" {
		if ids, err = readIDs(evalsetPath(*data, *setName)); err != nil {
			return err
		}
	} else {
		all, err := st.VideoStatsAll(ctx, *provider)
		if err != nil {
			return err
		}
		for _, v := range all {
			if v.Contributors >= *minContrib {
				ids = append(ids, v.NativeID)
			}
		}
	}

	inScope := map[string]bool{"sponsor": true, "self_promotion": true}
	type group struct {
		golds  []score.Gold
		preds  []score.Pred
		videos int
		humans int
	}
	groups := map[string]*group{"all": {}, "with assessor": {}, "no assessor": {}}
	for _, id := range ids {
		labels, err := st.Labels(ctx, *provider, id)
		if err != nil {
			return err
		}
		byHuman := map[string][]store.LabeledInterval{}
		hasAssessor := false
		for _, l := range labels {
			if !inScope[l.Category] {
				continue
			}
			byHuman[l.Contributor] = append(byHuman[l.Contributor], l)
			hasAssessor = hasAssessor || l.Assessor
		}
		if len(byHuman) < *minContrib {
			continue
		}
		var humans []string
		for h := range byHuman {
			humans = append(humans, h)
		}
		sort.Strings(humans)
		names := []string{"all", "no assessor"}
		if hasAssessor {
			names = []string{"all", "with assessor"}
		}
		for _, name := range names {
			g := groups[name]
			g.videos++
			for _, a := range humans {
				key := *provider + ":" + id + "|" + a // one pseudo-video per (video, predictor)
				g.humans++
				for _, l := range byHuman[a] {
					g.preds = append(g.preds, score.Pred{Video: key, Category: l.Category, Start: l.Start, End: l.End, Confidence: "high"})
				}
				for _, b := range humans {
					if b == a {
						continue
					}
					for _, l := range byHuman[b] {
						g.golds = append(g.golds, score.Gold{Video: key, Category: l.Category, Start: l.Start, End: l.End, Contributor: b, Assessor: l.Assessor})
					}
				}
			}
		}
	}

	opt := score.Options{MinSeconds: *minSeconds, IoU: *iou, MinConfidenceBy: productConfidence}
	fmt.Printf("human-human ceiling (each contributor vs the union of the others; lambda %g, min %g s)\n\n", *lambda, *minSeconds)
	fmt.Printf("%-14s %7s %9s %8s %8s %8s\n", "group", "videos", "predictors", "SCORE", "prec(s)", "recall(s)")
	for _, name := range []string{"all", "with assessor", "no assessor"} {
		g := groups[name]
		if g.videos == 0 {
			continue
		}
		res := score.Score(g.golds, g.preds, opt)
		x := res.BySeconds[score.AnyAd]
		fmt.Printf("%-14s %7d %9d %8.2f %8.2f %8.2f\n", name, g.videos, g.humans, score.NetScore(x, *lambda), x.Precision(), x.Recall())
	}
	return nil
}
