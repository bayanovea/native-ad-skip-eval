package main

import (
	"reflect"
	"testing"
)

var testRuns = []runConfig{ // newest first, as readRuns returns them
	{ID: "2026-11-02-model-b-v3", Model: "model-b", PromptVersion: "v3"},
	{ID: "2026-11-01-model-a-v3", Model: "model-a", PromptVersion: "v3"},
	{ID: "2026-10-30-model-a-v2", Model: "model-a", PromptVersion: "v2"},
}

func ids(runs []runConfig) []string {
	var out []string
	for _, r := range runs {
		out = append(out, r.ID)
	}
	return out
}

func TestFilterRuns(t *testing.T) {
	cases := []struct {
		name string
		in   listRunsIn
		want []string
	}{
		{"default limit keeps all", listRunsIn{}, ids(testRuns)},
		{"model substring, case-insensitive", listRunsIn{Model: "MODEL-A"}, []string{"2026-11-01-model-a-v3", "2026-10-30-model-a-v2"}},
		{"prompt is an exact version", listRunsIn{Prompt: "v3"}, []string{"2026-11-02-model-b-v3", "2026-11-01-model-a-v3"}},
		{"limit cuts newest first", listRunsIn{Limit: 1}, []string{"2026-11-02-model-b-v3"}},
		{"no match", listRunsIn{Model: "none"}, nil},
	}
	for _, c := range cases {
		if got := ids(filterRuns(testRuns, c.in)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClosestRuns(t *testing.T) {
	got := closestRuns(testRuns, "model-a-v2", 2)
	want := []string{"2026-10-30-model-a-v2", "2026-11-01-model-a-v3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := closestRuns(testRuns, "x", 5); len(got) != len(testRuns) {
		t.Errorf("n larger than the run count: got %d ids, want %d", len(got), len(testRuns))
	}
}
