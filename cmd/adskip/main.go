// Command adskip is the CLI for ad-skip-segment-eval.
//
//	adskip fetch    -set c2-spike       # subtitles -> transcripts (network, guarded)
//	adskip collapse -id <native_id>     # cached VTT -> transcript (offline)
//	adskip import   -file gold-v1.jsonl # gold export -> adskip.db (offline)
//	adskip evalset  heldout | v0        # eval-set files from the store (offline)
//	adskip detect   -set v0 -model ...  # transcripts -> predictions (model calls, paid)
//	adskip score    -run <id>           # predictions vs gold -> runs/<id>/score-*.json (offline)
//	adskip review   -file <tsv> -run .. # eye-check verdicts -> gold_reviews (offline)
//
// All private data (eval sets, subtitle cache, transcripts) lives under the data
// root (-data, default ~/Web/data), outside the repository.
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
	case "import":
		err = runImport(os.Args[2:])
	case "evalset":
		err = runEvalset(os.Args[2:])
	case "detect":
		err = runDetect(os.Args[2:])
	case "score":
		err = runScore(os.Args[2:])
	case "review":
		err = runReview(os.Args[2:])
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
  adskip fetch    [-provider main] [-set NAME] [-ids FILE] [-data DIR] [-lang ru] [-force] [-retries N] [-yt-dlp PATH] [-proxy URL] [id ...]
  adskip collapse [-provider main] [-data DIR] [-lang ru] -id NATIVE_ID
  adskip import   [-data DIR] -file GOLD_JSONL
  adskip evalset  heldout | v0 [-data DIR] [-n 10] [-min-contributors 3] [-exclude c2-spike]
  adskip detect   -set NAME [-model claude-opus-5-5] [-effort medium] [-prompt prompts/v1.md] [-limit N] [-dry-run] [-force] [-data DIR]
  adskip score    -run RUN_ID [-min-seconds 5] [-min-confidence low|medium|high] [-iou 0.5] [-gold all|assessor|reviewed] [-list] [-data DIR]
  adskip review   -file VERDICTS_TSV -run RUN_ID -date YYYY-MM-DD [-reviewer NAME] [-data DIR]`)
}

// defaultDataDir is the private data root: $ADSKIP_DATA or ~/Web/data.
func defaultDataDir() string { return envOr("ADSKIP_DATA", home("Web/data")) }

// cacheDir holds info.json, VTT and transcript files for one provider.
func cacheDir(data, provider string) string { return filepath.Join(data, "transcripts", provider) }

// evalsetPath resolves a set name to <data>/evalsets/<name>.txt; "" stays "".
func evalsetPath(data, name string) string {
	if name == "" {
		return ""
	}
	return filepath.Join(data, "evalsets", name+".txt")
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
