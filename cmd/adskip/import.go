package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"ad-skip-segment-eval/internal/gold"
	"ad-skip-segment-eval/internal/store"
)

// runImport loads a gold export (JSONL) into <data>/adskip.db. Re-running the
// same file is a no-op for labels; videos and contributors are upserted.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	file := fs.String("file", "", "gold export, one JSON object per line")
	data := fs.String("data", defaultDataDir(), "private data root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("-file is required")
	}
	batch, err := gold.ReadFile(*file)
	if err != nil {
		return err
	}
	for _, r := range batch.Rejected {
		fmt.Fprintln(os.Stderr, "rejected:", r)
	}
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	stats, err := st.Import(ctx, batch.Videos, batch.Contributors, batch.Labels)
	if err != nil {
		return err
	}
	v, c, l, err := st.Counts(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("read=%d rejected=%d inserted=%d duplicate=%d | store: videos=%d contributors=%d labels=%d -> %s\n",
		batch.Read, len(batch.Rejected), stats.Labels, stats.Duplicates, v, c, l, store.Path(*data))
	return nil
}
