package main

import (
	"context"
	"path/filepath"

	"github.com/bayanovea/ad-skip-segment-eval/internal/score"
	"github.com/bayanovea/ad-skip-segment-eval/internal/store"
)

// missRuns is what the key.tsv "runs_flagging" column says for a miss item:
// the stretch comes from human labels, not from a run.
const missRuns = "human_labels"

// minTextShare is the share of a miss stretch that must lie under subtitle
// cues; below it (a jingle, music) the text cannot decide and nobody is asked.
const minTextShare = 0.2

// missItems returns the reviewed-gold ad stretches (sponsor, self_promotion)
// of the runs' ok videos that no run keeps under the product thresholds, at
// least minSec long, not inside any earlier verdict region and with subtitle
// text under at least minTextShare of them.
func missItems(ctx context.Context, st *store.Store, data, provider string, runs []string, minSec float64, offsets map[string]float64, reviews []store.GoldReview) ([]sheetItem, error) {
	kept := map[string][]span{}
	videos := map[string]bool{}
	for _, r := range runs {
		ok, err := okSet(ctx, st, r)
		if err != nil {
			return nil, err
		}
		for id := range ok {
			videos[id] = true
		}
		ps, err := st.Predictions(ctx, r)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			if !ok[p.NativeID] || p.End-p.Start < minSec || !score.ConfidenceAtLeast(p.Confidence, productConfidence[p.Category]) {
				continue
			}
			if a, b, ok := shiftToPlayer(p.Start, p.End, offsets[p.Provider+":"+p.NativeID]); ok {
				kept[p.NativeID] = append(kept[p.NativeID], span{a, b})
			}
		}
	}
	var items []sheetItem
	for id := range videos {
		labels, err := st.Labels(ctx, provider, id)
		if err != nil {
			return nil, err
		}
		var golds []score.Gold
		for _, l := range labels {
			golds = append(golds, score.Gold{Video: id, Category: l.Category, Start: l.Start, End: l.End})
		}
		var revs []score.Review
		var reviewed []span
		for _, r := range reviews {
			if r.NativeID == id {
				revs = append(revs, score.Review{Video: id, Category: r.Category, Start: r.Start, End: r.End, Verdict: r.Verdict})
				reviewed = append(reviewed, span{r.Start, r.End})
			}
		}
		rg, _ := score.ApplyReviews(golds, nil, revs)
		var ads []span
		for _, g := range rg {
			if g.Category == "sponsor" || g.Category == "self_promotion" {
				ads = append(ads, span{g.Start, g.End})
			}
		}
		_, cues, err := readTranscript(filepath.Join(cacheDir(data, provider), id+".jsonl"))
		if err != nil {
			return nil, err
		}
		var text []span
		for _, c := range cues {
			if a, b, ok := shiftToPlayer(c.Start, c.End, offsets[provider+":"+id]); ok {
				text = append(text, span{a, b})
			}
		}
		text = mergeSpans(text)
		for _, m := range subtractSpans(mergeSpans(ads), mergeSpans(append(kept[id], reviewed...))) {
			if m.e-m.s < minSec || inside(m, text)/(m.e-m.s) < minTextShare {
				continue
			}
			it := sheetItem{video: id, start: m.s, end: m.e, cats: map[string]bool{}, runs: map[string]bool{missRuns: true}}
			for _, g := range rg {
				if (g.Category == "sponsor" || g.Category == "self_promotion") && g.Start < m.e && g.End > m.s {
					it.cats[g.Category] = true
				}
			}
			items = append(items, it)
		}
	}
	return items, nil
}

// subtractSpans returns the parts of a (merged, sorted) not covered by b (merged, sorted).
func subtractSpans(a, b []span) []span {
	var out []span
	for _, x := range a {
		cur := x.s
		for _, y := range b {
			if y.e <= cur || y.s >= x.e {
				continue
			}
			if y.s > cur {
				out = append(out, span{cur, y.s})
			}
			cur = max(cur, y.e)
		}
		if cur < x.e {
			out = append(out, span{cur, x.e})
		}
	}
	return out
}
