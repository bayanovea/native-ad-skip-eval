// Package score compares one run's predictions with the human labels of the
// same videos and turns the comparison into numbers.
//
// The unit is a segment, not a second: a prediction counts as a hit when it
// overlaps one gold segment of the same category by at least the IoU
// threshold, and every gold segment can be claimed by at most one prediction.
// Gold from several contributors is merged per category (union of overlapping
// intervals) before matching, because the product skips the union too.
//
// Thresholds (minimum duration, minimum confidence, IoU) are inputs, not
// constants: predictions are stored raw so one paid run can be scored under
// many settings. The package does not read the store; the caller passes
// plain intervals.
package score

import (
	"math"
	"sort"
)

// Category-agnostic view: every ad category collapses into this one, which is
// what an auto-skip user experiences (a sponsor skipped as self_promotion is
// still skipped).
const AnyAd = "any_ad"

// Confidence levels in rank order; the detector emits exactly these strings.
var confidenceRank = map[string]int{"low": 0, "medium": 1, "high": 2}

// ConfidenceAtLeast reports whether conf reaches min ("" passes everything).
func ConfidenceAtLeast(conf, min string) bool {
	return min == "" || confidenceRank[conf] >= confidenceRank[min]
}

// Gold is one human interval after import.
type Gold struct {
	Video       string // provider:native_id
	Category    string
	Start, End  float64
	Contributor string
	Assessor    bool
}

// Pred is one raw model prediction.
type Pred struct {
	Video      string
	Category   string
	Start, End float64
	Confidence string
}

// Options are the scoring settings; the zero value means "no filter".
type Options struct {
	MinSeconds    float64 // predictions shorter than this are dropped before matching
	MinConfidence string  // "", "low", "medium" or "high": predictions below are dropped
	// MinConfidenceBy overrides MinConfidence per category (the product rule:
	// sponsor from medium, self_promotion only high).
	MinConfidenceBy map[string]string
	IoU             float64 // minimum intersection-over-union for a hit
	Categories      []string
}

// Pair is one matched prediction/gold pair with its boundary errors in seconds
// (prediction minus gold: negative start means the prediction began earlier).
type Pair struct {
	Video      string
	Category   string
	IoU        float64
	StartDelta float64
	EndDelta   float64
	Confidence string
}

// Counts are the confusion numbers for one category.
type Counts struct {
	TP, FP, FN int
}

func (c Counts) Precision() float64 { return ratio(c.TP, c.TP+c.FP) }
func (c Counts) Recall() float64    { return ratio(c.TP, c.TP+c.FN) }
func (c Counts) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// Seconds is the time-weighted view of one category: how many seconds the
// detector would skip (Pred), how many the humans marked (Gold) and how many
// of them coincide (Both). It does not care how a block is cut into segments,
// which is exactly where the segment-level IoU rule punishes a model that
// splits one long gold block into pieces. Precision is the share of skipped
// seconds that were ads; recall is the share of ad seconds that were skipped.
type Seconds struct {
	Pred, Gold, Both float64
}

func (x Seconds) Precision() float64 { return fratio(x.Both, x.Pred) }
func (x Seconds) Recall() float64    { return fratio(x.Both, x.Gold) }

// WronglySkipped is content a viewer would lose (skipped, not an ad).
func (x Seconds) WronglySkipped() float64 { return x.Pred - x.Both }

// MissedAd is ad time that stays in the video.
func (x Seconds) MissedAd() float64 { return x.Gold - x.Both }

// Quantiles summarises a distribution of boundary errors.
type Quantiles struct {
	N               int
	MedianSigned    float64 // sign shows the direction: negative = prediction earlier
	AbsP50, AbsP90  float64
	WithinTolerance map[float64]float64 // tolerance seconds -> share of pairs within it
}

// Result is everything the scorer computes for one run under one Options.
type Result struct {
	Options      Options
	Videos       int
	PredsTotal   int            // before filtering
	PredsScored  int            // after filtering
	GoldMerged   map[string]int // category -> merged gold segments
	ByCategory   map[string]Counts
	BySeconds    map[string]Seconds
	VideoSeconds map[string]Seconds           // any_ad seconds per video, for paired comparisons
	ByConfidence map[string]map[string]Counts // category -> confidence -> counts (TP/FP only)
	Start, End   map[string]Quantiles         // category -> boundary error distribution
	Pairs        []Pair
	Unmatched    []Pred // false positives, for the review sheet
	Missed       []Gold // false negatives (merged gold), for the review sheet
}

// Score runs the comparison.
func Score(golds []Gold, preds []Pred, opt Options) Result {
	if len(opt.Categories) == 0 {
		opt.Categories = []string{"sponsor", "self_promotion"}
	}
	inScope := map[string]bool{}
	for _, c := range opt.Categories {
		inScope[c] = true
	}
	res := Result{
		Options:      opt,
		PredsTotal:   len(preds),
		GoldMerged:   map[string]int{},
		ByCategory:   map[string]Counts{},
		BySeconds:    map[string]Seconds{},
		ByConfidence: map[string]map[string]Counts{},
		Start:        map[string]Quantiles{},
		End:          map[string]Quantiles{},
	}

	// Filter predictions.
	var kept []Pred
	for _, p := range preds {
		if !inScope[p.Category] {
			continue
		}
		if p.End-p.Start < opt.MinSeconds {
			continue
		}
		min := opt.MinConfidence
		if c, ok := opt.MinConfidenceBy[p.Category]; ok {
			min = c
		}
		if min != "" && confidenceRank[p.Confidence] < confidenceRank[min] {
			continue
		}
		kept = append(kept, p)
	}
	res.PredsScored = len(kept)

	// Merge gold per video and category.
	merged := Merge(golds, inScope)
	videos := map[string]bool{}
	for _, g := range merged {
		videos[g.Video] = true
		res.GoldMerged[g.Category]++
	}
	for _, p := range kept {
		videos[p.Video] = true
	}
	res.Videos = len(videos)

	// Per category, plus the category-agnostic view.
	cats := append([]string{}, opt.Categories...)
	cats = append(cats, AnyAd)
	for _, cat := range cats {
		g := merged
		p := kept
		if cat == AnyAd {
			// Adjacent sponsor + self_promotion intervals become one on both
			// sides, which is what a skip button would do with them.
			g = Merge(relabel(merged, AnyAd), map[string]bool{AnyAd: true})
			p = mergePreds(relabelPreds(kept, AnyAd))
		} else {
			g = filterGold(merged, cat)
			p = filterPreds(kept, cat)
		}
		pairs, fp, fn := match(g, p, opt.IoU)
		c := Counts{TP: len(pairs), FP: len(fp), FN: len(fn)}
		res.ByCategory[cat] = c
		perVideo := secondsByVideo(g, p)
		res.BySeconds[cat] = sumSeconds(perVideo)
		if cat == AnyAd {
			res.VideoSeconds = perVideo
		}
		res.ByConfidence[cat] = byConfidence(pairs, fp)
		res.Start[cat] = quantiles(pairs, func(x Pair) float64 { return x.StartDelta })
		res.End[cat] = quantiles(pairs, func(x Pair) float64 { return x.EndDelta })
		if cat != AnyAd {
			res.Pairs = append(res.Pairs, pairs...)
			res.Unmatched = append(res.Unmatched, fp...)
			res.Missed = append(res.Missed, fn...)
		}
	}
	sort.Slice(res.Unmatched, func(i, j int) bool {
		return less(res.Unmatched[i].Video, res.Unmatched[i].Start, res.Unmatched[j].Video, res.Unmatched[j].Start)
	})
	sort.Slice(res.Missed, func(i, j int) bool {
		return less(res.Missed[i].Video, res.Missed[i].Start, res.Missed[j].Video, res.Missed[j].Start)
	})
	return res
}

// Merge unions overlapping or touching intervals of the same video and
// category across contributors. Contributor and Assessor are dropped: a
// merged interval has no single author.
func Merge(golds []Gold, inScope map[string]bool) []Gold {
	type key struct{ video, cat string }
	groups := map[key][]Gold{}
	for _, g := range golds {
		if inScope != nil && !inScope[g.Category] {
			continue
		}
		k := key{g.Video, g.Category}
		groups[k] = append(groups[k], g)
	}
	var out []Gold
	for k, gs := range groups {
		sort.Slice(gs, func(i, j int) bool { return gs[i].Start < gs[j].Start })
		cur := Gold{Video: k.video, Category: k.cat, Start: gs[0].Start, End: gs[0].End}
		for _, g := range gs[1:] {
			if g.Start <= cur.End {
				cur.End = math.Max(cur.End, g.End)
				continue
			}
			out = append(out, cur)
			cur = Gold{Video: k.video, Category: k.cat, Start: g.Start, End: g.End}
		}
		out = append(out, cur)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i].Video, out[i].Start, out[j].Video, out[j].Start) })
	return out
}

// match pairs predictions with gold one-to-one, greedily by IoU, within a video.
func match(golds []Gold, preds []Pred, minIoU float64) (pairs []Pair, fp []Pred, fn []Gold) {
	type cand struct {
		g, p int
		iou  float64
	}
	var cands []cand
	for gi, g := range golds {
		for pi, p := range preds {
			if g.Video != p.Video {
				continue
			}
			if v := IoU(g.Start, g.End, p.Start, p.End); v >= minIoU && v > 0 {
				cands = append(cands, cand{gi, pi, v})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].iou > cands[j].iou })
	usedG, usedP := map[int]bool{}, map[int]bool{}
	for _, c := range cands {
		if usedG[c.g] || usedP[c.p] {
			continue
		}
		usedG[c.g], usedP[c.p] = true, true
		g, p := golds[c.g], preds[c.p]
		pairs = append(pairs, Pair{Video: g.Video, Category: g.Category, IoU: c.iou, StartDelta: p.Start - g.Start, EndDelta: p.End - g.End, Confidence: p.Confidence})
	}
	for i, p := range preds {
		if !usedP[i] {
			fp = append(fp, p)
		}
	}
	for i, g := range golds {
		if !usedG[i] {
			fn = append(fn, g)
		}
	}
	return pairs, fp, fn
}

// IoU is intersection over union of two intervals; 0 when they do not overlap.
func IoU(aStart, aEnd, bStart, bEnd float64) float64 {
	inter := math.Min(aEnd, bEnd) - math.Max(aStart, bStart)
	if inter <= 0 {
		return 0
	}
	union := math.Max(aEnd, bEnd) - math.Min(aStart, bStart)
	if union <= 0 {
		return 0
	}
	return inter / union
}

func byConfidence(pairs []Pair, fp []Pred) map[string]Counts {
	out := map[string]Counts{}
	for _, p := range pairs {
		c := out[p.Confidence]
		c.TP++
		out[p.Confidence] = c
	}
	for _, p := range fp {
		c := out[p.Confidence]
		c.FP++
		out[p.Confidence] = c
	}
	return out
}

var tolerances = []float64{2, 5, 10}

func quantiles(pairs []Pair, f func(Pair) float64) Quantiles {
	q := Quantiles{N: len(pairs), WithinTolerance: map[float64]float64{}}
	if len(pairs) == 0 {
		return q
	}
	signed := make([]float64, len(pairs))
	abs := make([]float64, len(pairs))
	for i, p := range pairs {
		signed[i] = f(p)
		abs[i] = math.Abs(signed[i])
	}
	sort.Float64s(signed)
	sort.Float64s(abs)
	q.MedianSigned = percentile(signed, 0.5)
	q.AbsP50 = percentile(abs, 0.5)
	q.AbsP90 = percentile(abs, 0.9)
	for _, t := range tolerances {
		n := 0
		for _, a := range abs {
			if a <= t {
				n++
			}
		}
		q.WithinTolerance[t] = float64(n) / float64(len(abs))
	}
	return q
}

// percentile on a sorted slice, nearest-rank.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func less(v1 string, s1 float64, v2 string, s2 float64) bool {
	if v1 != v2 {
		return v1 < v2
	}
	return s1 < s2
}

func relabel(gs []Gold, cat string) []Gold {
	out := make([]Gold, len(gs))
	for i, g := range gs {
		g.Category = cat
		out[i] = g
	}
	return out
}

func relabelPreds(ps []Pred, cat string) []Pred {
	out := make([]Pred, len(ps))
	for i, p := range ps {
		p.Category = cat
		out[i] = p
	}
	return out
}

// mergePreds unions overlapping or touching predictions of one video (all
// already relabelled to one category). The merged prediction keeps the
// highest confidence of its parts.
func mergePreds(ps []Pred) []Pred {
	byVideo := map[string][]Pred{}
	for _, p := range ps {
		byVideo[p.Video] = append(byVideo[p.Video], p)
	}
	var out []Pred
	for _, group := range byVideo {
		sort.Slice(group, func(i, j int) bool { return group[i].Start < group[j].Start })
		cur := group[0]
		for _, p := range group[1:] {
			if p.Start <= cur.End {
				cur.End = math.Max(cur.End, p.End)
				if confidenceRank[p.Confidence] > confidenceRank[cur.Confidence] {
					cur.Confidence = p.Confidence
				}
				continue
			}
			out = append(out, cur)
			cur = p
		}
		out = append(out, cur)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i].Video, out[i].Start, out[j].Video, out[j].Start) })
	return out
}

func filterGold(gs []Gold, cat string) []Gold {
	var out []Gold
	for _, g := range gs {
		if g.Category == cat {
			out = append(out, g)
		}
	}
	return out
}

func filterPreds(ps []Pred, cat string) []Pred {
	var out []Pred
	for _, p := range ps {
		if p.Category == cat {
			out = append(out, p)
		}
	}
	return out
}

func sumSeconds(perVideo map[string]Seconds) Seconds {
	var r Seconds
	for _, x := range perVideo {
		r.Pred += x.Pred
		r.Gold += x.Gold
		r.Both += x.Both
	}
	return r
}

// secondsByVideo gives, per video, the union length of predictions, of gold and
// of their intersection. Both inputs are unioned first, so overlapping
// intervals are never counted twice.
func secondsByVideo(golds []Gold, preds []Pred) map[string]Seconds {
	type iv struct{ s, e float64 }
	gv, pv := map[string][]iv{}, map[string][]iv{}
	for _, g := range golds {
		gv[g.Video] = append(gv[g.Video], iv{g.Start, g.End})
	}
	for _, p := range preds {
		pv[p.Video] = append(pv[p.Video], iv{p.Start, p.End})
	}
	union := func(xs []iv) []iv {
		sort.Slice(xs, func(i, j int) bool { return xs[i].s < xs[j].s })
		var out []iv
		for _, x := range xs {
			if n := len(out); n > 0 && x.s <= out[n-1].e {
				out[n-1].e = math.Max(out[n-1].e, x.e)
				continue
			}
			out = append(out, x)
		}
		return out
	}
	out := map[string]Seconds{}
	videos := map[string]bool{}
	for v := range gv {
		videos[v] = true
	}
	for v := range pv {
		videos[v] = true
	}
	for v := range videos {
		var r Seconds
		g, p := union(gv[v]), union(pv[v])
		for _, x := range g {
			r.Gold += x.e - x.s
		}
		for _, x := range p {
			r.Pred += x.e - x.s
		}
		for _, a := range g {
			for _, b := range p {
				if lo, hi := math.Max(a.s, b.s), math.Min(a.e, b.e); hi > lo {
					r.Both += hi - lo
				}
			}
		}
		out[v] = r
	}
	return out
}

func fratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// NetScore is the one number used to compare variants: the share of all ad
// time that was removed, minus lambda times the content that was wrongly
// removed, both in seconds. A wrongly cut second counts lambda times worse than
// a missed ad second, because the player skips without asking (prompt purpose).
// 1.0 = every ad second removed and nothing else; 0 = what a viewer gets with
// no detector; below 0 = the detector costs more content than it saves ads.
// Without gold ad time the score is undefined and NetScore returns NaN, so a
// set with nothing to remove never reads as "no detector" (0).
func NetScore(x Seconds, lambda float64) float64 {
	if x.Gold == 0 {
		return math.NaN()
	}
	return (x.Both - lambda*x.WronglySkipped()) / x.Gold
}
