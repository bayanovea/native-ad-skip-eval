package provider

import (
	"testing"

	"ad-skip-segment-eval/internal/transcript"
	"ad-skip-segment-eval/internal/ytdlp"
)

func TestPickTrackShapes(t *testing.T) {
	manifest := ytdlp.Track{Ext: "vtt", URL: "u1", ManifestURL: "m1"}
	cases := []struct {
		name string
		subs []ytdlp.Track
		auto map[string][]ytdlp.Track
		want transcript.TrackType
		ok   bool
	}{
		{"auto label", []ytdlp.Track{manifest, {Ext: "Ru AUTO", URL: "u2"}}, nil, transcript.TrackAuto, true},
		{"plain label is not evidence", []ytdlp.Track{manifest, {Ext: "vtt", URL: "u2"}}, nil, transcript.TrackUnknown, true},
		{"manifest only", []ytdlp.Track{manifest}, nil, transcript.TrackUnknown, true},
		{"automatic_captions", nil, map[string][]ytdlp.Track{"ru": {{Ext: "vtt"}}}, transcript.TrackAuto, true},
		{"none", nil, nil, transcript.TrackNone, false},
	}
	for _, c := range cases {
		info := &ytdlp.Info{Subtitles: map[string][]ytdlp.Track{"ru": c.subs}, AutomaticCaptions: c.auto}
		_, typ, _, ok := platform{}.PickTrack(info, "ru")
		if typ != c.want || ok != c.ok {
			t.Errorf("%s: got %s/%v want %s/%v", c.name, typ, ok, c.want, c.ok)
		}
	}
}

func TestRegistry(t *testing.T) {
	p, err := Lookup("main")
	if err != nil || p.Name() != "main" {
		t.Fatalf("main not registered: %v", err)
	}
	if p.VideoURL("c1_2") != "https://example.invalid/video/c1_2" {
		t.Errorf("url: %s", p.VideoURL("c1_2"))
	}
	if _, err := Lookup("nope"); err == nil {
		t.Error("unknown provider must fail")
	}
}
