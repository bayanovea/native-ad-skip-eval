package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bayanovea/ad-skip-segment-eval/internal/store"
)

// flagTrackMismatch is the assessor's "subtitle track ≠ video" flag.
const flagTrackMismatch = "track_mismatch"

// runFlags loads the product's video-flags export into video_flags.
//
//	adskip flags -file <data>/review/video-flags.tsv
//
// TSV columns: provider, native_id, flag, value (true|false), reviewer, set_at.
func runFlags(args []string) error {
	fs := flag.NewFlagSet("flags", flag.ContinueOnError)
	file := fs.String("file", "", "video-flags TSV from the product (required)")
	data := fs.String("data", defaultDataDir(), "private data root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("-file is required")
	}
	f, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer f.Close()

	var flags []store.VideoFlag
	counts := map[string]int{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		if line == 1 && strings.HasPrefix(sc.Text(), "provider\t") {
			continue
		}
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) != 6 {
			return fmt.Errorf("line %d: want 6 tab-separated columns, got %d", line, len(cols))
		}
		if cols[2] != flagTrackMismatch {
			return fmt.Errorf("line %d: unknown flag %q", line, cols[2])
		}
		if cols[3] != "true" && cols[3] != "false" {
			return fmt.Errorf("line %d: value %q, want true or false", line, cols[3])
		}
		flags = append(flags, store.VideoFlag{Provider: cols[0], NativeID: cols[1], Flag: cols[2], Value: cols[3] == "true", Reviewer: cols[4], SetAt: cols[5]})
		counts[cols[2]+"="+cols[3]]++
	}
	if err := sc.Err(); err != nil {
		return err
	}

	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.SetFlags(context.Background(), flags)
	if err != nil {
		return err
	}
	fmt.Printf("flags: %d read, %d stored or updated; %v\n", len(flags), n, counts)
	return nil
}
