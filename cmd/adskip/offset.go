package main

import (
	"context"
	"flag"
	"fmt"

	"ad-skip-segment-eval/internal/store"
)

// runOffset records a per-video timeline offset.
//
//	adskip offset -id c1_1001 -seconds 11 -note "text 11 s later than in the player" -date 2026-10-12
//
// seconds = transcript time minus player time (positive: subtitles late).
func runOffset(args []string) error {
	fs := flag.NewFlagSet("offset", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	provider := fs.String("provider", "main", "video platform")
	id := fs.String("id", "", "native id (required)")
	seconds := fs.Float64("seconds", 0, "transcript time minus player time")
	note := fs.String("note", "", "evidence, e.g. which items showed it")
	date := fs.String("date", "", "date, YYYY-MM-DD (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *date == "" {
		return fmt.Errorf("-id and -date are required")
	}
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.SetOffset(context.Background(), *provider, *id, *seconds, *note, *date); err != nil {
		return err
	}
	fmt.Printf("offset %+.1f s set for %s:%s\n", *seconds, *provider, *id)
	return nil
}
