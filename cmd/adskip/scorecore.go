package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bayanovea/ad-skip-segment-eval/internal/score"
	"github.com/bayanovea/ad-skip-segment-eval/internal/store"
)

// productConfidence is the skip rule of the product: a sponsor segment from
// medium confidence, self-promotion only at high. The headline number is
// always computed under it, whatever -min-confidence says.
var productConfidence = map[string]string{"sponsor": "medium", "self_promotion": "high"}

type scoreParams struct {
	Gold         string // all | assessor | reviewed
	ExcludeOwner string
	Videos       map[string]bool // native ids to keep; nil = all (-videos)
	TimingClean  bool            // drop videos whose timeline is unreliable (timelineUnreliable)
	NoOffsets    bool
	DataRoot     string // needed by TimingClean to read transcripts
	Opt          score.Options
}

// computeScore scores one run against the gold of the videos it processed
// (status ok). It returns the result, the number of scored videos and the
// number of review verdicts applied.
func computeScore(ctx context.Context, st *store.Store, runID string, p scoreParams) (score.Result, int, int, error) {
	var zero score.Result
	videos, err := selectVideos(ctx, st, runID, p)
	if err != nil {
		return zero, 0, 0, err
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

// selectVideos returns the run's ok videos that pass the -exclude-owner,
// -videos and -timing-clean filters.
func selectVideos(ctx context.Context, st *store.Store, runID string, p scoreParams) ([][2]string, error) {
	videos, err := st.RunVideos(ctx, runID, "ok")
	if err != nil {
		return nil, err
	}
	if len(videos) == 0 {
		return nil, fmt.Errorf("run %s has no ok videos in the store", runID)
	}
	var mismatch map[string]bool
	if p.TimingClean {
		if mismatch, err = st.Flags(ctx, flagTrackMismatch); err != nil {
			return nil, err
		}
	}
	var kept [][2]string
	for _, v := range videos {
		if p.ExcludeOwner != "" && strings.HasPrefix(v[1], p.ExcludeOwner+"_") {
			continue
		}
		if p.Videos != nil && !p.Videos[v[1]] {
			continue
		}
		if p.TimingClean && timelineUnreliable(mismatch, p.DataRoot, v[0], v[1]) {
			continue
		}
		kept = append(kept, v)
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("no videos left after the filters")
	}
	return kept, nil
}

// readVideoSet reads a -videos file (one native id per line, # comments, as
// in evalsets/); nil for an empty path.
func readVideoSet(path string) (map[string]bool, error) {
	if path == "" {
		return nil, nil
	}
	ids, err := readIDs(path)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// timelineUnreliable says whether a video's subtitle times cannot be compared
// with player-time gold: either signal is enough. An assessor's "matches" does
// not override the overrun, because it covers only the items they checked.
func timelineUnreliable(mismatch map[string]bool, data, provider, id string) bool {
	return mismatch[provider+":"+id] || transcriptOverrun(data, provider, id) > overrunLimit
}

// overrunLimit is the number of seconds by which the last subtitle cue may
// end after the video. Measured against the audio on 29 videos (2026-10-13):
// 11 of 14 videos above 3 s were shifted by more than 2 s, 1 of 15 below.
const overrunLimit = 3.0

// transcriptOverrun is the end of the last cue minus the video duration, from
// the cached transcript (meta line + cues); 0 when unknown.
func transcriptOverrun(data, provider, id string) float64 {
	f, err := os.Open(filepath.Join(data, "transcripts", provider, id+".jsonl"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var duration, lastEnd float64
	for sc.Scan() {
		var l struct {
			Kind     string  `json:"kind"`
			Duration float64 `json:"duration"`
			End      float64 `json:"end"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Kind {
		case "meta":
			duration = l.Duration
		case "cue":
			lastEnd = l.End
		}
	}
	if duration == 0 || lastEnd == 0 {
		return 0
	}
	return lastEnd - duration
}
