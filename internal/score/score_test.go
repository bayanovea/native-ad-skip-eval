package score

import (
	"math"
	"testing"
)

const v = "main:c1_1001"

func TestMergeUnionsOverlapsAcrossContributors(t *testing.T) {
	golds := []Gold{
		{Video: v, Category: "sponsor", Start: 10, End: 20, Contributor: "a"},
		{Video: v, Category: "sponsor", Start: 15, End: 30, Contributor: "b"},
		{Video: v, Category: "sponsor", Start: 30, End: 35, Contributor: "c"}, // touching: merged
		{Video: v, Category: "sponsor", Start: 100, End: 110, Contributor: "a"},
		{Video: v, Category: "self_promotion", Start: 12, End: 18, Contributor: "a"}, // other category: separate
	}
	got := Merge(golds, nil)
	if len(got) != 3 {
		t.Fatalf("merged = %d intervals, want 3: %+v", len(got), got)
	}
	if got[0].Start != 10 || got[0].End != 35 || got[0].Category != "sponsor" {
		t.Errorf("first merged = %+v, want sponsor 10-35", got[0])
	}
}

func TestScoreCountsAndBoundaryErrors(t *testing.T) {
	golds := []Gold{
		{Video: v, Category: "sponsor", Start: 100, End: 160},
		{Video: v, Category: "sponsor", Start: 500, End: 520}, // missed
		{Video: v, Category: "self_promotion", Start: 900, End: 930},
	}
	preds := []Pred{
		{Video: v, Category: "sponsor", Start: 103, End: 158, Confidence: "high"},          // hit: +3 / -2
		{Video: v, Category: "sponsor", Start: 300, End: 310, Confidence: "medium"},        // false positive
		{Video: v, Category: "self_promotion", Start: 905, End: 928, Confidence: "low"},    // hit
		{Video: v, Category: "self_promotion", Start: 1000, End: 1003, Confidence: "high"}, // 3 s: filtered
	}
	res := Score(golds, preds, Options{MinSeconds: 5, IoU: 0.5})

	if res.PredsTotal != 4 || res.PredsScored != 3 {
		t.Errorf("preds total/scored = %d/%d, want 4/3", res.PredsTotal, res.PredsScored)
	}
	sp := res.ByCategory["sponsor"]
	if sp != (Counts{TP: 1, FP: 1, FN: 1}) {
		t.Errorf("sponsor = %+v, want TP1 FP1 FN1", sp)
	}
	if p := sp.Precision(); math.Abs(p-0.5) > 1e-9 {
		t.Errorf("sponsor precision = %v, want 0.5", p)
	}
	self := res.ByCategory["self_promotion"]
	if self != (Counts{TP: 1, FP: 0, FN: 0}) {
		t.Errorf("self_promotion = %+v, want TP1", self)
	}
	any := res.ByCategory[AnyAd]
	if any != (Counts{TP: 2, FP: 1, FN: 1}) {
		t.Errorf("any_ad = %+v, want TP2 FP1 FN1", any)
	}
	st := res.Start["sponsor"]
	if st.N != 1 || st.MedianSigned != 3 || st.AbsP50 != 3 {
		t.Errorf("sponsor start errors = %+v, want median +3", st)
	}
	en := res.End["sponsor"]
	if en.MedianSigned != -2 {
		t.Errorf("sponsor end errors = %+v, want median -2", en)
	}
	if st.WithinTolerance[2] != 0 || st.WithinTolerance[5] != 1 {
		t.Errorf("tolerance shares = %v, want 0 within 2 s and 1 within 5 s", st.WithinTolerance)
	}
	if len(res.Unmatched) != 1 || res.Unmatched[0].Start != 300 {
		t.Errorf("unmatched = %+v, want the 300-310 prediction", res.Unmatched)
	}
	if len(res.Missed) != 1 || res.Missed[0].Start != 500 {
		t.Errorf("missed = %+v, want the 500-520 gold", res.Missed)
	}
	if c := res.ByConfidence["sponsor"]["medium"]; c.FP != 1 {
		t.Errorf("medium confidence counts = %+v, want FP1", c)
	}
}

func TestScoreConfidenceFilterAndCategoryDispute(t *testing.T) {
	golds := []Gold{{Video: v, Category: "sponsor", Start: 100, End: 160}}
	preds := []Pred{{Video: v, Category: "self_promotion", Start: 101, End: 159, Confidence: "low"}}

	res := Score(golds, preds, Options{IoU: 0.5})
	if res.ByCategory["sponsor"].FN != 1 || res.ByCategory["self_promotion"].FP != 1 {
		t.Errorf("per-category should see a miss and a false positive: %+v", res.ByCategory)
	}
	if res.ByCategory[AnyAd] != (Counts{TP: 1}) {
		t.Errorf("any_ad should count a category dispute as a hit: %+v", res.ByCategory[AnyAd])
	}

	res = Score(golds, preds, Options{IoU: 0.5, MinConfidence: "medium"})
	if res.PredsScored != 0 || res.ByCategory[AnyAd].FN != 1 {
		t.Errorf("low-confidence prediction should be filtered: scored=%d any=%+v", res.PredsScored, res.ByCategory[AnyAd])
	}
}

func TestAnyAdMergesAdjacentPredictions(t *testing.T) {
	golds := []Gold{{Video: v, Category: "sponsor", Start: 1266, End: 1336}}
	preds := []Pred{
		{Video: v, Category: "self_promotion", Start: 1267, End: 1301, Confidence: "medium"},
		{Video: v, Category: "self_promotion", Start: 1301, End: 1335, Confidence: "medium"},
	}
	res := Score(golds, preds, Options{IoU: 0.5})
	if res.ByCategory[AnyAd] != (Counts{TP: 1}) {
		t.Errorf("two touching predictions over one gold span should be one hit in any_ad: %+v", res.ByCategory[AnyAd])
	}
}

func TestIoU(t *testing.T) {
	cases := []struct{ a, b, c, d, want float64 }{
		{0, 10, 0, 10, 1},
		{0, 10, 5, 15, 1.0 / 3},
		{0, 10, 10, 20, 0},
		{0, 10, 20, 30, 0},
	}
	for _, c := range cases {
		if got := IoU(c.a, c.b, c.c, c.d); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("IoU(%v,%v,%v,%v) = %v, want %v", c.a, c.b, c.c, c.d, got, c.want)
		}
	}
}

func TestApplyReviews(t *testing.T) {
	golds := []Gold{
		{Video: v, Category: "sponsor", Start: 0, End: 30},        // gold_wrong: removed
		{Video: v, Category: "sponsor", Start: 100, End: 160},     // untouched
		{Video: v, Category: "sponsor", Start: 500, End: 560},     // definition_unclear: removed
		{Video: v, Category: "self_promotion", Start: 5, End: 25}, // other category: untouched
	}
	preds := []Pred{
		{Video: v, Category: "sponsor", Start: 505, End: 555, Confidence: "high"},        // definition_unclear: removed
		{Video: v, Category: "self_promotion", Start: 900, End: 930, Confidence: "high"}, // gold_missing: now a hit
	}
	reviews := []Review{
		{Video: v, Category: "sponsor", Start: 0, End: 31, Verdict: VerdictGoldWrong, Item: "B2"},
		{Video: v, Category: "sponsor", Start: 500, End: 560, Verdict: VerdictDefinitionUnclear, Item: "B7"},
		{Video: v, Category: "self_promotion", Start: 900, End: 930, Verdict: VerdictGoldMissing, Item: "A3"},
		{Video: v, Category: "sponsor", Start: 100, End: 160, Verdict: VerdictModelWrong, Item: "B1"},
	}
	g, p := ApplyReviews(golds, preds, reviews)
	if len(g) != 3 {
		t.Fatalf("gold after reviews = %d, want 3: %+v", len(g), g)
	}
	if len(p) != 1 || p[0].Start != 900 {
		t.Fatalf("preds after reviews = %+v, want only the 900-930 one", p)
	}
	res := Score(g, p, Options{IoU: 0.3})
	if res.ByCategory["self_promotion"] != (Counts{TP: 1, FN: 1}) { // the untouched 5-25 label stays a miss
		t.Errorf("self_promotion = %+v, want TP1 after gold_missing", res.ByCategory["self_promotion"])
	}
	if res.ByCategory["sponsor"] != (Counts{FN: 1}) {
		t.Errorf("sponsor = %+v, want only the untouched 100-160 miss", res.ByCategory["sponsor"])
	}
}

func TestSecondsOverlapIgnoresHowBlocksAreCut(t *testing.T) {
	golds := []Gold{{Video: "v", Category: "sponsor", Start: 100, End: 400}}
	// One long gold block (300 s) predicted as two pieces: IoU rule would miss both,
	// seconds see 220 s of 300 s covered and no wrongly skipped time.
	preds := []Pred{
		{Video: "v", Category: "sponsor", Start: 110, End: 200},
		{Video: "v", Category: "sponsor", Start: 250, End: 380},
	}
	r := Score(golds, preds, Options{IoU: 0.5, MinSeconds: 5})
	sec := r.BySeconds["sponsor"]
	if sec.Both != 220 || sec.Pred != 220 || sec.Gold != 300 {
		t.Fatalf("seconds = %+v", sec)
	}
	if sec.Precision() != 1 || math.Abs(sec.Recall()-220.0/300) > 1e-9 {
		t.Fatalf("precision %v recall %v", sec.Precision(), sec.Recall())
	}
	if r.ByCategory["sponsor"].TP != 0 {
		t.Fatalf("segment level should miss both pieces at IoU 0.5, got %+v", r.ByCategory["sponsor"])
	}
}
