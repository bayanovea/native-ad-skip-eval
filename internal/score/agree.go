package score

// AgreeOptions are the settings of Agree.
type AgreeOptions struct {
	Served  Options // what the first model would serve: MinSeconds, MinConfidence(By)
	IoU     float64 // the product's confirmation threshold: same category, IoU at least this
	GoldIoU float64 // a served segment is a true ad when it overlaps merged gold of its category by this much
}

// Agreement is what a second model's veto does to the first model's served
// segments: Confirmed + Unconfirmed = served, split by whether gold backs them.
// TPVetoed is the recall the veto costs, FPVetoed the wrong cuts it saves.
type Agreement struct {
	Served, Confirmed, Unconfirmed int
	TPConfirmed, TPVetoed          int
	FPConfirmed, FPVetoed          int
	SecondOnly                     int // second-model segments matching no first-model segment of their category
}

// Agree applies the product's two-model rule (ai/pipeline/confirm.go in the
// product): a served first-model segment stays served when a second-model
// segment of the same video and category overlaps it with IoU >= opt.IoU.
// The second model's predictions are not filtered.
func Agree(golds []Gold, first, second []Pred, opt AgreeOptions) Agreement {
	merged := Merge(golds, nil)
	var a Agreement
	for _, p := range first {
		if !served(p, opt.Served) {
			continue
		}
		a.Served++
		tp := overlapsAny(p, merged, opt.GoldIoU)
		switch confirmed := matchesAny(p, second, opt.IoU); {
		case confirmed && tp:
			a.Confirmed++
			a.TPConfirmed++
		case confirmed:
			a.Confirmed++
			a.FPConfirmed++
		case tp:
			a.Unconfirmed++
			a.TPVetoed++
		default:
			a.Unconfirmed++
			a.FPVetoed++
		}
	}
	for _, s := range second {
		if !matchesAny(s, first, opt.IoU) {
			a.SecondOnly++
		}
	}
	return a
}

// ServedConfirmed is what the product serves of the first model under the
// two-model rule: the first model's served segments a second-model segment confirms.
func ServedConfirmed(first, second []Pred, opt AgreeOptions) []Pred {
	var out []Pred
	for _, p := range first {
		if served(p, opt.Served) && matchesAny(p, second, opt.IoU) {
			out = append(out, p)
		}
	}
	return out
}

func served(p Pred, opt Options) bool {
	if p.End-p.Start < opt.MinSeconds {
		return false
	}
	min := opt.MinConfidence
	if m, ok := opt.MinConfidenceBy[p.Category]; ok {
		min = m
	}
	return ConfidenceAtLeast(p.Confidence, min)
}

func matchesAny(p Pred, others []Pred, minIoU float64) bool {
	for _, o := range others {
		if o.Video == p.Video && o.Category == p.Category && IoU(p.Start, p.End, o.Start, o.End) >= minIoU {
			return true
		}
	}
	return false
}

func overlapsAny(p Pred, golds []Gold, minIoU float64) bool {
	for _, g := range golds {
		if g.Video == p.Video && g.Category == p.Category && IoU(p.Start, p.End, g.Start, g.End) >= minIoU {
			return true
		}
	}
	return false
}
