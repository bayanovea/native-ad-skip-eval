package main

import (
	"context"
	"fmt"
	"strings"

	"ad-skip-segment-eval/internal/score"
	"ad-skip-segment-eval/internal/store"
)

// productConfidence is the skip rule of the product: a sponsor segment from
// medium confidence, self-promotion only at high. The headline number is
// always computed under it, whatever -min-confidence says.
var productConfidence = map[string]string{"sponsor": "medium", "self_promotion": "high"}

type scoreParams struct {
	Gold         string // all | assessor | reviewed
	ExcludeOwner string
	NoOffsets    bool
	Opt          score.Options
}

// computeScore scores one run against the gold of the videos it processed
// (status ok). It returns the result, the number of scored videos and the
// number of review verdicts applied.
func computeScore(ctx context.Context, st *store.Store, runID string, p scoreParams) (score.Result, int, int, error) {
	var zero score.Result
	videos, err := st.RunVideos(ctx, runID, "ok")
	if err != nil {
		return zero, 0, 0, err
	}
	if len(videos) == 0 {
		return zero, 0, 0, fmt.Errorf("run %s has no ok videos in the store", runID)
	}
	// In assessor mode a video without any assessor label has no gold at all,
	// so it is left out entirely rather than scored as "no ads here".
	okVideo := map[string]bool{}
	var kept [][2]string
	for _, v := range videos {
		if p.ExcludeOwner != "" && strings.HasPrefix(v[1], p.ExcludeOwner+"_") {
			continue
		}
		kept = append(kept, v)
	}
	videos = kept
	if len(videos) == 0 {
		return zero, 0, 0, fmt.Errorf("no videos left after -exclude-owner %s", p.ExcludeOwner)
	}
	var golds []score.Gold
	scored := 0
	for _, v := range videos {
		key := v[0] + ":" + v[1]
		labels, err := st.Labels(ctx, v[0], v[1])
		if err != nil {
			return zero, 0, 0, err
		}
		var vg []score.Gold
		hasAssessor := false
		for _, l := range labels {
			hasAssessor = hasAssessor || l.Assessor
			if p.Gold == "assessor" && !l.Assessor {
				continue
			}
			vg = append(vg, score.Gold{Video: key, Category: l.Category, Start: l.Start, End: l.End, Contributor: l.Contributor, Assessor: l.Assessor})
		}
		if p.Gold == "assessor" && !hasAssessor {
			continue
		}
		okVideo[key] = true
		scored++
		golds = append(golds, vg...)
	}
	rawPreds, err := st.Predictions(ctx, runID)
	if err != nil {
		return zero, 0, 0, err
	}
	offsets, err := st.Offsets(ctx)
	if err != nil {
		return zero, 0, 0, err
	}
	if p.NoOffsets {
		offsets = nil
	}
	var preds []score.Pred
	for _, p := range rawPreds {
		key := p.Provider + ":" + p.NativeID
		if !okVideo[key] {
			continue // predictions of a call that later failed validation are not scored
		}
		a, b, ok := shiftToPlayer(p.Start, p.End, offsets[key]) // transcript minus player time; 0 when unknown
		if !ok {
			continue
		}
		preds = append(preds, score.Pred{Video: key, Category: p.Category, Start: a, End: b, Confidence: p.Confidence})
	}

	reviewsApplied := 0
	if p.Gold == "reviewed" {
		stored, err := st.Reviews(ctx)
		if err != nil {
			return zero, 0, 0, err
		}
		var reviews []score.Review
		for _, r := range stored {
			if key := r.Provider + ":" + r.NativeID; okVideo[key] {
				reviews = append(reviews, score.Review{Video: key, Category: r.Category, Start: r.Start, End: r.End, Verdict: r.Verdict, Item: r.Item, Note: r.Note})
			}
		}
		if len(reviews) == 0 {
			return zero, 0, 0, fmt.Errorf("-gold reviewed: no stored verdicts for this run's videos (adskip review)")
		}
		golds, preds = score.ApplyReviews(golds, preds, reviews)
		reviewsApplied = len(reviews)
	}
	return score.Score(golds, preds, p.Opt), scored, reviewsApplied, nil
}
