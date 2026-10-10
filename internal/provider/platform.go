package provider

import (
	"strings"

	"ad-skip-segment-eval/internal/transcript"
	"ad-skip-segment-eval/internal/ytdlp"
)

// Native ids look like "<owner>_<video>".
type platform struct{}

func init() { Register(platform{}) }

func (platform) Name() string                    { return "main" }
func (platform) VideoURL(nativeID string) string { return "https://example.invalid/video/" + nativeID }

// PickTrack prefers an author-uploaded track and falls back to auto-subtitles.
//
// The platform's yt-dlp extractor lists two entries per language under `subtitles`: one
// derived from the stream manifest (manifest_url set, ext "vtt", no label) and
// one from the player's subtitle list, where `ext` carries the player's label.
// A label containing "auto" means auto-subtitles. A plain "vtt" label is NOT
// evidence of an uploaded track: a video labelled "Автоматические" in the player
// showed up as "vtt" too (checked 2026-10-10), so anything else stays unknown.
func (platform) PickTrack(info *ytdlp.Info, lang string) (t ytdlp.Track, typ transcript.TrackType, auto bool, ok bool) {
	subs := info.Subtitles[lang]
	if len(subs) > 0 {
		typ = transcript.TrackUnknown
		t = subs[0]
		for _, tr := range subs {
			if tr.ManifestURL != "" {
				continue // manifest-derived entry: format only, no label
			}
			t = tr
			if looksAuto(tr) {
				typ = transcript.TrackAuto
			}
			break
		}
		return t, typ, false, true
	}
	if trs := info.AutomaticCaptions[lang]; len(trs) > 0 {
		return trs[0], transcript.TrackAuto, true, true
	}
	return ytdlp.Track{}, transcript.TrackNone, false, false
}

func looksAuto(t ytdlp.Track) bool {
	s := strings.ToLower(t.Name + " " + t.Ext)
	return strings.Contains(s, "auto") || strings.Contains(s, "автомат")
}
