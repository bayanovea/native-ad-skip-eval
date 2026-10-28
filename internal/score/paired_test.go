package score

import (
	"math"
	"strings"
	"testing"
)

func byVideo(v string) string { return v }

func TestPairedDiffIdenticalRuns(t *testing.T) {
	runs := map[string]Seconds{"p:a_1": {Gold: 100, Pred: 60, Both: 50}, "p:b_1": {Gold: 200, Pred: 100, Both: 100}}
	p, err := PairedDiff(runs, runs, byVideo, 10, 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Diff != 0 || p.Lo != 0 || p.Hi != 0 || p.Changed != 0 {
		t.Fatalf("identical runs: %+v", p)
	}
}

func TestPairedDiffIsRatioOfSums(t *testing.T) {
	base := map[string]Seconds{"p:a_1": {Gold: 100, Pred: 50, Both: 50}, "p:b_1": {Gold: 300, Pred: 0, Both: 0}}
	// Candidate finds 100 more ad seconds in b_1 and cuts 2 s of content there.
	cand := map[string]Seconds{"p:a_1": {Gold: 100, Pred: 50, Both: 50}, "p:b_1": {Gold: 300, Pred: 102, Both: 100}}
	p, err := PairedDiff(base, cand, byVideo, 10, 2000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := (100.0 - 10*2) / 400; math.Abs(p.Diff-want) > 1e-9 {
		t.Fatalf("diff = %v, want %v", p.Diff, want)
	}
	if p.Changed != 1 || math.Abs(p.MaxShare-1) > 1e-9 || p.Units != 2 {
		t.Fatalf("changed %d, max share %v, units %d", p.Changed, p.MaxShare, p.Units)
	}
	// Resamples without b_1 give 0, so the interval must reach 0.
	if p.Lo != 0 || p.Hi <= p.Diff {
		t.Fatalf("interval [%v, %v] around %v", p.Lo, p.Hi, p.Diff)
	}
}

func TestPairedDiffClusterResamplesWholeChannel(t *testing.T) {
	base := map[string]Seconds{"p:a_1": {Gold: 100}, "p:a_2": {Gold: 100}}
	cand := map[string]Seconds{"p:a_1": {Gold: 100, Pred: 100, Both: 100}, "p:a_2": {Gold: 100}}
	channel := func(v string) string { id := v[strings.Index(v, ":")+1:]; return id[:strings.Index(id, "_")] }
	p, err := PairedDiff(base, cand, channel, 10, 500, 1)
	if err != nil {
		t.Fatal(err)
	}
	// One channel: every resample is the whole set, so the interval collapses.
	if p.Units != 1 || p.Lo != p.Diff || p.Hi != p.Diff {
		t.Fatalf("one channel: %+v", p)
	}
}

func TestPairedDiffRefusesDifferentGold(t *testing.T) {
	base := map[string]Seconds{"p:a_1": {Gold: 100}}
	cand := map[string]Seconds{"p:a_1": {Gold: 120}}
	if _, err := PairedDiff(base, cand, byVideo, 10, 10, 1); err == nil {
		t.Fatal("want an error when gold differs")
	}
}
