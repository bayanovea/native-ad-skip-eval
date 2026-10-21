package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bayanovea/ad-skip-segment-eval/internal/provider"
	"github.com/bayanovea/ad-skip-segment-eval/internal/transcript"
	"github.com/bayanovea/ad-skip-segment-eval/internal/vtt"
)

// runCollapse rebuilds <id>.jsonl from a cached VTT without any network access.
// Duration/title come from a cached info.json when present.
func runCollapse(args []string) error {
	fs := flag.NewFlagSet("collapse", flag.ContinueOnError)
	providerName := fs.String("provider", "main", "video platform")
	id := fs.String("id", "", "native_id of a video whose VTT is already cached")
	data := fs.String("data", defaultDataDir(), "private data root")
	lang := fs.String("lang", "ru", "subtitle language")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("-id is required")
	}
	if _, err := provider.Lookup(*providerName); err != nil {
		return err
	}
	cache := cacheDir(*data, *providerName)
	base := filepath.Join(cache, *id)
	vttPath := findVTT(base, *lang)
	if vttPath == "" {
		return fmt.Errorf("no %s.%s*.vtt in %s", *id, *lang, cache)
	}
	cues, err := collapseFile(vttPath)
	if err != nil {
		return err
	}
	meta := transcript.Meta{NativeID: *id, Provider: *providerName, Lang: *lang,
		TrackType: transcript.TrackUntyped, FetchedAt: time.Now().UTC()}
	if raw, err := os.ReadFile(base + ".info.json"); err == nil {
		var info struct {
			Title    string  `json:"title"`
			Duration float64 `json:"duration"`
		}
		if json.Unmarshal(raw, &info) == nil {
			meta.Title, meta.Duration = info.Title, info.Duration
		}
	}
	out := base + ".jsonl"
	if err := transcript.WriteFile(out, meta, cues); err != nil {
		return err
	}
	fmt.Printf("%s: cues=%d words=%d -> %s\n", *id, len(cues), vtt.Words(cues), out)
	return nil
}
