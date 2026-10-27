package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bayanovea/native-ad-skip-eval/internal/store"
)

// heldoutBuckets / heldoutOf: a video is held out when its hash bucket (of 5)
// is 0, i.e. a fixed 20% of assessor videos. Decided before any prompt run;
// never inspected during prompt iteration; reported once per milestone.
const heldoutBuckets, heldoutBucket = 5, 0

// runEvalset writes eval-set files into <data>/evalsets from the store.
//
//	adskip evalset heldout            # 20% of assessor videos, by hash
//	adskip evalset v0 [-n 10]         # first N assessor + N multi-contributor videos not held out
func runEvalset(args []string) error {
	if len(args) < 1 {
		return errors.New("evalset needs a kind: heldout | v0 | dev")
	}
	kind := args[0]
	if kind == "clean-cand" || kind == "clean" {
		return runEvalsetClean(kind, args[1:])
	}
	fs := flag.NewFlagSet("evalset "+kind, flag.ContinueOnError)
	providerName := providerFlag(fs)
	data := fs.String("data", defaultDataDir(), "private data root")
	n := fs.Int("n", 10, "v0: videos per stratum")
	minContrib := fs.Int("min-contributors", 3, "v0: multi-contributor stratum threshold")
	exclude := fs.String("exclude", "c2-spike", "v0/dev: comma-separated set names whose ids are skipped")
	perChannel := fs.Int("per-channel", 3, "dev: at most this many assessor videos per channel")
	outside := fs.Int("outside", 0, "dev: extra videos from channels without any assessor video (>= 2 contributors, one per channel); 0 = none")
	minMin := fs.Float64("min-minutes", 10, "dev: skip videos with a known duration below this")
	maxMin := fs.Float64("max-minutes", 60, "dev: skip videos with a known duration above this (unknown durations pass and are checked after the fetch)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if _, err := resolveProvider(providerName); err != nil {
		return err
	}
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	all, err := st.VideoStatsAll(context.Background(), *providerName)
	if err != nil {
		return err
	}
	isHeldout := func(v store.VideoStats) bool {
		return v.Assessor && store.Bucket(v.Provider, v.NativeID, heldoutBuckets) == heldoutBucket
	}

	var ids []string
	var header string
	switch kind {
	case "heldout":
		for _, v := range all {
			if isHeldout(v) {
				ids = append(ids, v.NativeID)
			}
		}
		header = fmt.Sprintf("rule: assessor videos with sha256(provider:id) bucket %d of %d; frozen before the first prompt run; never inspect during prompt iteration",
			heldoutBucket, heldoutBuckets)
	case "v0":
		skip := map[string]bool{}
		for _, name := range strings.Split(*exclude, ",") {
			if name = strings.TrimSpace(name); name != "" {
				ex, err := readIDs(evalsetPath(*data, name))
				if err != nil {
					return fmt.Errorf("exclude set %s: %w", name, err)
				}
				for _, id := range ex {
					skip[id] = true
				}
			}
		}
		store.SortByHash(all)
		var assessor, multi []string
		for _, v := range all {
			if isHeldout(v) || skip[v.NativeID] {
				continue
			}
			switch {
			case v.Assessor && len(assessor) < *n:
				assessor = append(assessor, v.NativeID)
			case !v.Assessor && v.Contributors >= *minContrib && len(multi) < *n:
				multi = append(multi, v.NativeID)
			}
		}
		if len(assessor) < *n || len(multi) < *n {
			return fmt.Errorf("not enough videos: assessor=%d multi=%d (want %d each)", len(assessor), len(multi), *n)
		}
		ids = append(ids, "# stratum: assessor-labelled")
		ids = append(ids, assessor...)
		ids = append(ids, fmt.Sprintf("# stratum: >= %d contributors, no assessor", *minContrib))
		ids = append(ids, multi...)
		header = fmt.Sprintf("rule: first %d per stratum in sha256 order, not held out, not in %s", *n, *exclude)
	case "dev":
		skip := map[string]bool{}
		for _, name := range strings.Split(*exclude, ",") {
			if name = strings.TrimSpace(name); name != "" {
				ex, err := readIDs(evalsetPath(*data, name))
				if err != nil {
					return fmt.Errorf("exclude set %s: %w", name, err)
				}
				for _, id := range ex {
					skip[id] = true
				}
			}
		}
		channel := func(id string) string { return id[:strings.LastIndex(id, "_")] }
		okDuration := func(v store.VideoStats) bool {
			return v.Duration == 0 || (v.Duration >= *minMin*60 && v.Duration <= *maxMin*60)
		}
		assessorChannels := map[string]bool{}
		for _, v := range all {
			if v.Assessor {
				assessorChannels[channel(v.NativeID)] = true
			}
		}
		store.SortByHash(all)
		perCh := map[string]int{}
		var inChannel, outChannel []string
		seenOut := map[string]bool{}
		for _, v := range all {
			if isHeldout(v) || skip[v.NativeID] || !okDuration(v) {
				continue
			}
			ch := channel(v.NativeID)
			switch {
			case v.Assessor && perCh[ch] < *perChannel:
				perCh[ch]++
				inChannel = append(inChannel, v.NativeID)
			case !v.Assessor && !assessorChannels[ch] && v.Contributors >= 2 && !seenOut[ch] && len(outChannel) < *outside:
				seenOut[ch] = true
				outChannel = append(outChannel, v.NativeID)
			}
		}
		ids = append(ids, "# stratum: assessor-labelled, at most "+fmt.Sprint(*perChannel)+" per channel")
		ids = append(ids, inChannel...)
		if len(outChannel) > 0 {
			ids = append(ids, "# stratum: outside the assessor channels, >= 2 contributors, one per channel (report as a separate row)")
			ids = append(ids, outChannel...)
		}
		header = fmt.Sprintf("rule: sha256 order, not held out, not in %s, known duration %g-%g min, %d per channel", *exclude, *minMin, *maxMin, *perChannel)
	default:
		return fmt.Errorf("unknown evalset kind %q", kind)
	}

	out := evalsetPath(*data, kind)
	if err := writeSet(out, header, ids); err != nil {
		return err
	}
	count := 0
	for _, id := range ids {
		if !strings.HasPrefix(id, "#") {
			count++
		}
	}
	fmt.Printf("%s: %d videos -> %s\n", kind, count, out)
	return nil
}

// writeSet writes the file atomically; content is byte-identical across runs
// except for the generated-at line, so a diff shows real changes only.
func writeSet(path, header string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "# %s\n# generated %s by adskip evalset\n", header, time.Now().UTC().Format("2006-01-02"))
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
