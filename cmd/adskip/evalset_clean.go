package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/bayanovea/native-ad-skip-eval/internal/netguard"
	"github.com/bayanovea/native-ad-skip-eval/internal/store"
	"github.com/bayanovea/native-ad-skip-eval/internal/transcript"
)

// runEvalsetClean builds the clean-video slice: videos from the same channels as
// a dev set that nobody has marked. The product keeps only marked videos, so the
// labelled set contains no video without ads and the cost of wrong cuts where
// there is nothing to cut is otherwise unmeasured.
//
//	adskip evalset clean-cand -from dev [-per-channel 2] [-list-end 200]   # lists channels (network, VPN guard)
//	adskip fetch -set clean-cand                                           # durations and subtitles
//	adskip evalset clean -from dev [-n 20]                                 # first N in hash order, 1 per channel
//
// "Unmarked" is not "has no ads": nobody marked the video, which is the state
// the product's players meet. The slice is judged by eye on what the detector
// cuts, not against gold.
func runEvalsetClean(kind string, args []string) error {
	fs := flag.NewFlagSet("evalset "+kind, flag.ContinueOnError)
	providerName := fs.String("provider", "main", "video platform")
	data := fs.String("data", defaultDataDir(), "private data root")
	from := fs.String("from", "dev", "eval set whose channels are sampled")
	perChannel := fs.Int("per-channel", 2, "clean-cand: candidates kept per channel")
	listEnd := fs.Int("list-end", 200, "clean-cand: newest uploads read per channel")
	n := fs.Int("n", 20, "clean: videos in the slice")
	minMin := fs.Float64("min-minutes", 10, "clean: skip videos shorter than this")
	maxMin := fs.Float64("max-minutes", 60, "clean: skip videos longer than this")
	bin := fs.String("yt-dlp", envOr("YT_DLP", "yt-dlp"), "yt-dlp binary")
	homeIPs := fs.String("home-ips", home(".config/native-ad-skip-eval/home_ips"), "file listing IPs the listing must never run from")
	if err := fs.Parse(args); err != nil {
		return err
	}
	src, err := readIDs(evalsetPath(*data, *from))
	if err != nil {
		return fmt.Errorf("from set %s: %w", *from, err)
	}
	channelOf := func(id string) string { return id[:strings.LastIndex(id, "_")] }
	var channels []string
	seen := map[string]bool{}
	for _, id := range src {
		if ch := channelOf(id); !seen[ch] {
			seen[ch] = true
			channels = append(channels, ch)
		}
	}

	switch kind {
	case "clean-cand":
		return cleanCandidates(*data, *providerName, *bin, *homeIPs, channels, *perChannel, *listEnd, *from)
	case "clean":
		return cleanPick(*data, *providerName, *n, *minMin, *maxMin, *from)
	}
	return fmt.Errorf("unknown evalset kind %q", kind)
}

func cleanCandidates(data, providerName, bin, homeIPs string, channels []string, perChannel, listEnd int, from string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ip, err := netguard.Verify(ctx, homeIPs, netguard.DefaultLookupURL)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "external IP %s: ok\n", ip)

	st, err := store.Open(store.Path(data))
	if err != nil {
		return err
	}
	defer st.Close()
	marked, err := st.VideoStatsAll(ctx, providerName)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, v := range marked {
		known[v.NativeID] = true
	}

	var ids []string
	for _, ch := range channels {
		// The playlist URL is the owner's list of all uploads.
		url := fmt.Sprintf("https://example.invalid/playlist/%s_-2", ch)
		lctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		out, err := exec.CommandContext(lctx, bin, "--flat-playlist", "--playlist-end", fmt.Sprint(listEnd),
			"--no-warnings", "--print", "%(id)s", url).Output()
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%-14s listing failed: %v\n", ch, err)
			continue
		}
		var vs []store.VideoStats
		listed := 0
		for _, id := range strings.Fields(string(out)) {
			listed++
			if !strings.HasPrefix(id, ch+"_") || known[id] {
				continue
			}
			vs = append(vs, store.VideoStats{Provider: providerName, NativeID: id})
		}
		store.SortByHash(vs)
		if len(vs) > perChannel {
			vs = vs[:perChannel]
		}
		fmt.Printf("%-14s listed=%-4d candidates=%d\n", ch, listed, len(vs))
		for _, v := range vs {
			ids = append(ids, v.NativeID)
		}
	}
	if len(ids) == 0 {
		return errors.New("no candidates")
	}
	header := fmt.Sprintf("rule: newest %d uploads of each %s channel, minus every video that has a label, %d per channel in sha256 order; next: fetch, then evalset clean", listEnd, from, perChannel)
	if err := writeSet(evalsetPath(data, "clean-cand"), header, ids); err != nil {
		return err
	}
	fmt.Printf("clean-cand: %d videos -> %s\n", len(ids), evalsetPath(data, "clean-cand"))
	return nil
}

func cleanPick(data, providerName string, n int, minMin, maxMin float64, from string) error {
	cands, err := readIDs(evalsetPath(data, "clean-cand"))
	if err != nil {
		return fmt.Errorf("clean-cand: %w (run evalset clean-cand and fetch first)", err)
	}
	vs := make([]store.VideoStats, len(cands))
	for i, id := range cands {
		vs[i] = store.VideoStats{Provider: providerName, NativeID: id}
	}
	store.SortByHash(vs)
	usedChannel := map[string]bool{}
	var ids []string
	skipped := map[string]int{}
	for _, v := range vs {
		if len(ids) >= n {
			break
		}
		ch := v.NativeID[:strings.LastIndex(v.NativeID, "_")]
		if usedChannel[ch] {
			continue
		}
		m, _, err := readTranscript(fmt.Sprintf("%s/%s.jsonl", cacheDir(data, providerName), v.NativeID))
		switch {
		case err != nil:
			skipped["not fetched"]++
		case m.TrackType == transcript.TrackNone:
			skipped["no subtitles"]++
		case m.Duration < minMin*60 || m.Duration > maxMin*60:
			skipped["duration out of range"]++
		default:
			usedChannel[ch] = true
			ids = append(ids, v.NativeID)
		}
	}
	if len(ids) < n {
		fmt.Fprintf(os.Stderr, "only %d of %d found; skipped: %v\n", len(ids), n, skipped)
	}
	header := fmt.Sprintf("rule: from clean-cand (unmarked videos of the %s channels), sha256 order, one per channel, subtitles present, %g-%g min; skipped: %v", from, minMin, maxMin, skipped)
	if err := writeSet(evalsetPath(data, "clean"), header, ids); err != nil {
		return err
	}
	fmt.Printf("clean: %d videos -> %s\n", len(ids), evalsetPath(data, "clean"))
	return nil
}
