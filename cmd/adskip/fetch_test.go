package main

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
		{"plain label", []ytdlp.Track{manifest, {Ext: "vtt", URL: "u2"}}, nil, transcript.TrackManual, true},
		{"manifest only", []ytdlp.Track{manifest}, nil, transcript.TrackUnknown, true},
		{"automatic_captions", nil, map[string][]ytdlp.Track{"ru": {{Ext: "vtt"}}}, transcript.TrackAuto, true},
		{"none", nil, nil, transcript.TrackNone, false},
	}
	for _, c := range cases {
		info := &ytdlp.Info{Subtitles: map[string][]ytdlp.Track{"ru": c.subs}, AutomaticCaptions: c.auto}
		_, typ, _, ok := pickTrack(info, "ru")
		if typ != c.want || ok != c.ok {
			t.Errorf("%s: got %s/%v want %s/%v", c.name, typ, ok, c.want, c.ok)
		}
	}
}
