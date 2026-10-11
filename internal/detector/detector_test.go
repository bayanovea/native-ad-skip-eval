package detector

import (
	"strings"
	"testing"

	"ad-skip-segment-eval/internal/transcript"
	"ad-skip-segment-eval/internal/vtt"
)

var cues = []vtt.Cue{
	{Start: 0, End: 2.4, Text: "всем привет"},
	{Start: 2.4, End: 5.1, Text: "сегодня у нас"},
	{Start: 9.0, End: 12.0, Text: "спонсор выпуска, промокод"},
	{Start: 12.0, End: 15.5, Text: "ссылка в описании"},
	{Start: 20.0, End: 22.0, Text: "а теперь к теме"},
}

func TestNewRequestRendersNumberedCues(t *testing.T) {
	p := Prompt{Version: "v1", Text: "SYSTEM"}
	req := NewRequest(p, transcript.Meta{Lang: "ru", Duration: 22.4}, cues)
	if req.System != "SYSTEM" || req.Schema != Schema {
		t.Fatal("system or schema not passed through")
	}
	want := []string{`<transcript lang="ru" duration="22">`, "[2] 9.0-12.0: спонсор выпуска, промокод", "</transcript>"}
	for _, w := range want {
		if !strings.Contains(req.User, w) {
			t.Errorf("user message lacks %q:\n%s", w, req.User)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Output{Segments: []Segment{{Category: "sponsor", FirstCue: 2, LastCue: 3, Confidence: "high", Quote: "спонсор выпуска"}}}
	if err := Validate(cues, ok); err != nil {
		t.Fatalf("valid output rejected: %v", err)
	}
	if err := Validate(cues, Output{}); err != nil {
		t.Fatalf("empty output rejected: %v", err)
	}
	bad := map[string]Output{
		"index out of range": {Segments: []Segment{{Category: "sponsor", FirstCue: 2, LastCue: 5}}},
		"reversed":           {Segments: []Segment{{Category: "sponsor", FirstCue: 3, LastCue: 2}}},
		"overlap":            {Segments: []Segment{{Category: "sponsor", FirstCue: 1, LastCue: 2}, {Category: "self_promotion", FirstCue: 2, LastCue: 3}}},
		"unknown category":   {Segments: []Segment{{Category: "intro", FirstCue: 0, LastCue: 1}}},
	}
	for name, out := range bad {
		if err := Validate(cues, out); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// adjacent segments are fine: sponsor then self_promotion back to back
	adj := Output{Segments: []Segment{{Category: "self_promotion", FirstCue: 3, LastCue: 3}, {Category: "sponsor", FirstCue: 2, LastCue: 2}}}
	if err := Validate(cues, adj); err != nil {
		t.Errorf("adjacent segments rejected: %v", err)
	}
}

func TestResolveAndQuote(t *testing.T) {
	out := Output{Segments: []Segment{{Category: "sponsor", FirstCue: 2, LastCue: 3, Quote: "Спонсор выпуска"}}}
	preds := Resolve(cues, out)
	if len(preds) != 1 || preds[0].Start != 9.0 || preds[0].End != 15.5 {
		t.Fatalf("resolve: %+v", preds)
	}
	if !QuoteMatches(cues, out.Segments[0]) {
		t.Error("case-insensitive prefix quote should match")
	}
	if QuoteMatches(cues, Segment{FirstCue: 2, Quote: "ссылка"}) {
		t.Error("quote from another cue should not match")
	}
}

func TestParseOutputAndCost(t *testing.T) {
	out, err := ParseOutput(`{"segments":[{"category":"sponsor","first_cue":2,"last_cue":3,"confidence":"low","quote":"x"}]}`)
	if err != nil || len(out.Segments) != 1 {
		t.Fatalf("parse: %v %+v", err, out)
	}
	if _, err := ParseOutput(`{"segments":[],"extra":1}`); err == nil {
		t.Error("unknown field accepted")
	}
	c := Prices["claude-opus-5-5"].Cost(Usage{InputTokens: 1_000_000, OutputTokens: 100_000})
	if c != 6 {
		t.Errorf("cost: want 6, got %v", c)
	}
}
