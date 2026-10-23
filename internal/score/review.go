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
// touches intervals of the same video and category that overlap a review, and
// only the reviewed stretch of them: a gold_wrong or definition_unclear verdict
// on the uncovered rest of a label (a miss sheet item) must not throw away the
// part the model did find, or that prediction turns into a false positive.
func ApplyReviews(golds []Gold, preds []Pred, reviews []Review) ([]Gold, []Pred) {
	var outG []Gold
	for _, g := range golds {
		for _, s := range cut(g.Video, g.Category, g.Start, g.End, reviews, VerdictGoldWrong, VerdictDefinitionUnclear) {
			c := g
			c.Start, c.End = s[0], s[1]
			outG = append(outG, c)
		}
	}
	for _, r := range reviews {
		if r.Verdict == VerdictGoldMissing {
			outG = append(outG, Gold{Video: r.Video, Category: r.Category, Start: r.Start, End: r.End, Contributor: "review:" + r.Item})
		}
	}
	var outP []Pred
	for _, p := range preds {
		for _, s := range cut(p.Video, p.Category, p.Start, p.End, reviews, VerdictDefinitionUnclear) {
			c := p
			c.Start, c.End = s[0], s[1]
			outP = append(outP, c)
		}
	}
	return outG, outP
}

// cut subtracts every matching review's stretch from [start, end) and returns
// what is left, in order; nothing when the reviews cover it.
func cut(video, cat string, start, end float64, reviews []Review, verdicts ...string) [][2]float64 {
	left := [][2]float64{{start, end}}
	for _, r := range reviews {
		if r.Video != video || r.Category != cat || !hasVerdict(r.Verdict, verdicts) {
			continue
		}
		var next [][2]float64
		for _, s := range left {
			if s[0] >= r.End || r.Start >= s[1] {
				next = append(next, s)
				continue
			}
			if s[0] < r.Start {
				next = append(next, [2]float64{s[0], r.Start})
			}
			if r.End < s[1] {
				next = append(next, [2]float64{r.End, s[1]})
			}
		}
		left = next
	}
	return left
}

func hasVerdict(v string, vs []string) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
