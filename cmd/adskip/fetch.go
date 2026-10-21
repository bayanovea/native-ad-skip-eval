package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/bayanovea/ad-skip-segment-eval/internal/netguard"
	"github.com/bayanovea/ad-skip-segment-eval/internal/provider"
	"github.com/bayanovea/ad-skip-segment-eval/internal/transcript"
	"github.com/bayanovea/ad-skip-segment-eval/internal/vtt"
	"github.com/bayanovea/ad-skip-segment-eval/internal/ytdlp"
)

type fetchOpts struct {
	data, lang, homeIPs, bin, proxy string
	retries                         int
	force                           bool
	provider                        provider.Provider
}

type result struct {
	id     string
	status string // cached | ok | no-subs | failed
	cues   int
	words  int
	err    error
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	var o fetchOpts
	providerName := fs.String("provider", "main", "video platform: "+strings.Join(provider.Names(), ", "))
	set := fs.String("set", "", "eval set name: reads <data>/evalsets/<name>.txt")
	idsFile := fs.String("ids", "", "file with one native_id per line (# comments allowed)")
	fs.StringVar(&o.data, "data", defaultDataDir(), "private data root (eval sets, subtitle cache, transcripts); never inside the repo")
	fs.StringVar(&o.lang, "lang", "ru", "subtitle language")
	fs.StringVar(&o.homeIPs, "home-ips", home(".config/ad-skip-segment-eval/home_ips"), "file listing IPs fetch must never run from")
	fs.StringVar(&o.bin, "yt-dlp", envOr("YT_DLP", "yt-dlp"), "yt-dlp binary")
	fs.StringVar(&o.proxy, "proxy", "", "passed to yt-dlp --proxy")
	fs.IntVar(&o.retries, "retries", 3, "attempts per yt-dlp call")
	fs.BoolVar(&o.force, "force", false, "refetch even if cached")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := provider.Lookup(*providerName)
	if err != nil {
		return err
	}
	o.provider = p
	ids := fs.Args()
	for _, path := range []string{evalsetPath(o.data, *set), *idsFile} {
		if path == "" {
			continue
		}
		fromFile, err := readIDs(path)
		if err != nil {
			return err
		}
		ids = append(ids, fromFile...)
	}
	if len(ids) == 0 {
		return errors.New("no ids: pass -set NAME, -ids FILE or native_ids as arguments")
	}
	if err := os.MkdirAll(cacheDir(o.data, p.Name()), 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	f := &fetcher{opts: o, runner: &ytdlp.Runner{
		Bin: o.bin, Proxy: o.proxy, Attempts: o.retries, Backoff: 2 * time.Second,
		Logf: func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) },
	}}

	var results []result
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		r := f.one(ctx, id)
		results = append(results, r)
		line := fmt.Sprintf("%-24s %-8s", r.id, r.status)
		if r.status == "ok" || r.status == "cached" {
			line += fmt.Sprintf(" cues=%-5d words=%d", r.cues, r.words)
		}
		if r.err != nil {
			line += " " + r.err.Error()
		}
		fmt.Println(line)
		if errors.Is(r.err, netguard.ErrRefused) {
			break // the IP will not change mid-run; stop instead of failing each id
		}
	}
	return summarize(results, len(ids))
}

func summarize(results []result, total int) error {
	counts := map[string]int{}
	for _, r := range results {
		counts[r.status]++
	}
	fmt.Printf("\n%d/%d done: ok=%d cached=%d no-subs=%d failed=%d\n",
		len(results), total, counts["ok"], counts["cached"], counts["no-subs"], counts["failed"])
	if counts["failed"] > 0 || len(results) < total {
		return fmt.Errorf("%d failed, %d not attempted; rerun to retry (cached ones are skipped)",
			counts["failed"], total-len(results))
	}
	return nil
}

type fetcher struct {
	opts    fetchOpts
	runner  *ytdlp.Runner
	guarded bool // IP verified for this run
}

func (f *fetcher) paths(id string) (jsonl, info, vttBase string) {
	d := cacheDir(f.opts.data, f.opts.provider.Name())
	return filepath.Join(d, id+".jsonl"), filepath.Join(d, id+".info.json"), filepath.Join(d, id)
}

func (f *fetcher) one(ctx context.Context, id string) result {
	r := result{id: id}
	jsonlPath, infoPath, vttBase := f.paths(id)
	url := f.opts.provider.VideoURL(id)

	if !f.opts.force {
		// A transcript built offline by `collapse` has no metadata yet and is completed here.
		if m, cues, err := readTranscript(jsonlPath); err == nil && m.TrackType != transcript.TrackUntyped {
			r.status, r.cues, r.words = "cached", len(cues), m.Words
			if m.TrackType == transcript.TrackNone {
				r.status = "no-subs"
			}
			return r
		}
	}

	// 1. metadata (cached unless -force)
	var info *ytdlp.Info
	if raw, err := os.ReadFile(infoPath); err == nil && !f.opts.force {
		info = &ytdlp.Info{}
		if err := json.Unmarshal(raw, info); err != nil {
			info = nil
		}
	}
	if info == nil {
		if err := f.guard(ctx); err != nil {
			return failed(r, err)
		}
		var raw []byte
		var err error
		info, raw, err = f.runner.Info(ctx, url)
		if err != nil {
			return failed(r, err)
		}
		if err := os.WriteFile(infoPath, raw, 0o644); err != nil {
			return failed(r, err)
		}
	}

	meta := transcript.Meta{
		NativeID: id, Provider: f.opts.provider.Name(), Title: info.Title, Duration: info.Duration,
		Lang: f.opts.lang, FetchedAt: time.Now().UTC(),
	}

	// 2. pick a track: author-uploaded first, auto as fallback
	track, trackType, auto, ok := f.opts.provider.PickTrack(info, f.opts.lang)
	if !ok {
		meta.TrackType = transcript.TrackNone
		if err := transcript.WriteFile(jsonlPath, meta, nil); err != nil {
			return failed(r, err)
		}
		r.status = "no-subs"
		return r
	}
	meta.TrackType = trackType
	meta.TrackName = strings.TrimSpace(track.Name + " " + track.Ext) // the player label, e.g. "Ru AUTO"

	// 3. VTT (cached unless -force)
	vttPath := findVTT(vttBase, f.opts.lang)
	if vttPath == "" || f.opts.force {
		if err := f.guard(ctx); err != nil {
			return failed(r, err)
		}
		if err := f.runner.WriteSubs(ctx, url, vttBase, f.opts.lang, auto); err != nil {
			return failed(r, err)
		}
		if vttPath = findVTT(vttBase, f.opts.lang); vttPath == "" {
			return failed(r, fmt.Errorf("yt-dlp reported a track but wrote no %s.*.vtt", filepath.Base(vttBase)))
		}
	}

	// 4. collapse
	cues, err := collapseFile(vttPath)
	if err != nil {
		return failed(r, err)
	}
	if err := transcript.WriteFile(jsonlPath, meta, cues); err != nil {
		return failed(r, err)
	}
	r.status, r.cues, r.words = "ok", len(cues), vtt.Words(cues)
	return r
}

// guard verifies the external IP once per run, right before the first network call.
func (f *fetcher) guard(ctx context.Context) error {
	if f.guarded {
		return nil
	}
	ip, err := netguard.Verify(ctx, f.opts.homeIPs, netguard.DefaultLookupURL)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "external IP %s: ok\n", ip)
	f.guarded = true
	return nil
}

func failed(r result, err error) result {
	r.status, r.err = "failed", err
	return r
}

// findVTT returns the first "<base>.<lang>*.vtt" (yt-dlp may add a track suffix).
func findVTT(base, lang string) string {
	matches, _ := filepath.Glob(base + "." + lang + "*.vtt")
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func collapseFile(path string) ([]vtt.Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := vtt.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return vtt.Collapse(raw), nil
}

func readTranscript(path string) (transcript.Meta, []vtt.Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return transcript.Meta{}, nil, err
	}
	defer f.Close()
	return transcript.Read(f)
}

func readIDs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}
	return ids, sc.Err()
}
