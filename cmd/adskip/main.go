// Command adskip is the CLI for ad-skip-segment-eval.
//
//	adskip fetch    -ids data/evalset/c2-spike.txt   # subtitles -> transcripts (network, guarded)
//	adskip collapse -id <native_id>                  # cached VTT -> transcript (offline)
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fetch":
		err = runFetch(os.Args[2:])
	case "collapse":
		err = runCollapse(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  adskip fetch    [-ids FILE] [-cache DIR] [-lang ru] [-force] [-retries N] [-yt-dlp PATH] [-proxy URL] [id ...]
  adskip collapse [-cache DIR] [-lang ru] -id NATIVE_ID`)
}

func home(rel string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		return rel
	}
	return filepath.Join(h, rel)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
