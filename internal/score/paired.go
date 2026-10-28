package score

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// Paired is the SCORE difference candidate − base on the same videos and the
// same gold, with a 95% percentile bootstrap interval. SCORE is a ratio of
// sums over videos, so the resample recomputes the ratio rather than averaging
// per-video scores: a video with 20 s of ad would otherwise weigh as much as
// one with 20 minutes.
type Paired struct {
	Base, Candidate, Diff float64
	Lo, Hi                float64
	Units                 int     // clusters resampled
	Changed               int     // videos whose contribution to Diff is not zero
	MaxShare              float64 // largest single-video |contribution| / |Diff|
}

// PairedDiff resamples whole clusters (cluster maps a video key to its unit:
// the video itself, or its channel), because seconds inside one cluster are
// not independent. Both runs must see the same gold per video, otherwise the
// difference mixes the model change with a gold change.
func PairedDiff(base, cand map[string]Seconds, cluster func(video string) string, lambda float64, iters int, seed uint64) (Paired, error) {
	videos := map[string]bool{}
	for v := range base {
		videos[v] = true
	}
	for v := range cand {
		videos[v] = true
	}
	type sums struct{ base, cand, gold float64 }
	byUnit := map[string]*sums{}
	var total sums
	contrib := map[string]float64{}
	for v := range videos {
		b, c := base[v], cand[v]
		if math.Abs(b.Gold-c.Gold) > 1e-6 {
			return Paired{}, fmt.Errorf("video %s: gold differs between runs (%.1f s vs %.1f s)", v, b.Gold, c.Gold)
		}
		nb, nc := b.Both-lambda*b.WronglySkipped(), c.Both-lambda*c.WronglySkipped()
		u := byUnit[cluster(v)]
		if u == nil {
			u = &sums{}
			byUnit[cluster(v)] = u
		}
		u.base, u.cand, u.gold = u.base+nb, u.cand+nc, u.gold+b.Gold
		total.base, total.cand, total.gold = total.base+nb, total.cand+nc, total.gold+b.Gold
		contrib[v] = nc - nb
	}
	if total.gold == 0 {
		return Paired{}, fmt.Errorf("no gold ad time in the videos")
	}
	p := Paired{
		Base:      total.base / total.gold,
		Candidate: total.cand / total.gold,
		Units:     len(byUnit),
	}
	p.Diff = p.Candidate - p.Base
	for _, d := range contrib {
		if math.Abs(d) > 1e-9 {
			p.Changed++
		}
		if p.Diff != 0 {
			p.MaxShare = max(p.MaxShare, math.Abs(d/total.gold/p.Diff))
		}
	}

	keys := make([]string, 0, len(byUnit))
	for k := range byUnit {
		keys = append(keys, k)
	}
	sort.Strings(keys) // map order is random; the interval must be reproducible from the seed
	units := make([]sums, len(keys))
	for i, k := range keys {
		units[i] = *byUnit[k]
	}
	rng := rand.New(rand.NewPCG(seed, seed))
	diffs := make([]float64, 0, iters)
	for len(diffs) < iters {
		var s sums
		for range units {
			u := units[rng.IntN(len(units))]
			s.base, s.cand, s.gold = s.base+u.base, s.cand+u.cand, s.gold+u.gold
		}
		if s.gold == 0 {
			continue
		}
		diffs = append(diffs, (s.cand-s.base)/s.gold)
	}
	sort.Float64s(diffs)
	p.Lo, p.Hi = percentile(diffs, 0.025), percentile(diffs, 0.975)
	return p, nil
}
