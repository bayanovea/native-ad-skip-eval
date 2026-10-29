package score

import "testing"

func TestAgree(t *testing.T) {
	golds := []Gold{{Video: "v", Category: "sponsor", Start: 100, End: 160}}
	first := []Pred{
		{Video: "v", Category: "sponsor", Start: 100, End: 160, Confidence: "high"},          // gold, confirmed
		{Video: "v", Category: "sponsor", Start: 300, End: 340, Confidence: "medium"},        // no gold, vetoed
		{Video: "v", Category: "self_promotion", Start: 500, End: 560, Confidence: "medium"}, // below the product rule
		{Video: "v", Category: "sponsor", Start: 700, End: 702, Confidence: "high"},          // too short
	}
	second := []Pred{
		{Video: "v", Category: "sponsor", Start: 105, End: 165, Confidence: "low"},
		{Video: "v", Category: "self_promotion", Start: 300, End: 340, Confidence: "high"}, // other category: no confirmation
		{Video: "w", Category: "sponsor", Start: 300, End: 340, Confidence: "high"},        // other video
	}
	opt := AgreeOptions{Served: Options{MinSeconds: 5, MinConfidenceBy: map[string]string{"sponsor": "medium", "self_promotion": "high"}}, IoU: 0.5, GoldIoU: 0.3}
	got := Agree(golds, first, second, opt)
	want := Agreement{Served: 2, Confirmed: 1, Unconfirmed: 1, TPConfirmed: 1, FPVetoed: 1, SecondOnly: 2}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// The gold ad's second segment overlaps at IoU 50/70: a stricter threshold vetoes the true ad.
	opt.IoU = 0.9
	if got := Agree(golds, first, second, opt); got.TPVetoed != 1 || got.Confirmed != 0 {
		t.Fatalf("IoU 0.9: %+v", got)
	}
}
