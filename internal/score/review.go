package score

// Review verdicts are a second, auditable layer over the raw labels: a person
// looked at one disagreement between a run and the gold and said who was
// right. The raw labels are never edited; the scorer applies verdicts on top
// when asked, so a number can be reported against raw gold and against
// reviewed gold side by side.

// Verdict values, as stored.
const (
	VerdictGoldMissing       = "gold_missing"       // the model found a real ad the humans did not label: add it to gold
	VerdictGoldWrong         = "gold_wrong"         // the human label is not an ad: remove it from gold
	VerdictDefinitionUnclear = "definition_unclear" // reasonable people disagree: exclude the region from both sides
	VerdictModelWrong        = "model_wrong"        // the model missed or invented: gold stands (no change)
	VerdictGoldValid         = "gold_valid"         // the label is right but not detectable from text (no change)
)

// Review is one verdict on one interval of one video.
type Review struct {
	Video      string
	Category   string
	Start, End float64
	Verdict    string
	Item       string // sheet item id, e.g. "A3"
	Note       string
}

// ApplyReviews returns gold and predictions adjusted by the verdicts. It only
// touches intervals of the same video and category that overlap a review.
func ApplyReviews(golds []Gold, preds []Pred, reviews []Review) ([]Gold, []Pred) {
	var outG []Gold
	for _, g := range golds {
		if !overlapsAny(g.Video, g.Category, g.Start, g.End, reviews, VerdictGoldWrong, VerdictDefinitionUnclear) {
			outG = append(outG, g)
		}
	}
	for _, r := range reviews {
		if r.Verdict == VerdictGoldMissing {
			outG = append(outG, Gold{Video: r.Video, Category: r.Category, Start: r.Start, End: r.End, Contributor: "review:" + r.Item})
		}
	}
	var outP []Pred
	for _, p := range preds {
		if !overlapsAny(p.Video, p.Category, p.Start, p.End, reviews, VerdictDefinitionUnclear) {
			outP = append(outP, p)
		}
	}
	return outG, outP
}

func overlapsAny(video, cat string, start, end float64, reviews []Review, verdicts ...string) bool {
	for _, r := range reviews {
		if r.Video != video || r.Category != cat {
			continue
		}
		if !hasVerdict(r.Verdict, verdicts) {
			continue
		}
		if start < r.End && r.Start < end {
			return true
		}
	}
	return false
}

func hasVerdict(v string, vs []string) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
