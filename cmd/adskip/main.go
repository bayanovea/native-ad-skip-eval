// Command adskip is the CLI for native-ad-skip-eval.
//
//	adskip fetch    -set c2-spike       # subtitles -> transcripts (network, guarded)
//	adskip collapse -id <native_id>     # cached VTT -> transcript (offline)
//	adskip import   -file gold-v1.jsonl # gold export -> adskip.db (offline)
//	adskip evalset  heldout | v0        # eval-set files from the store (offline)
//	adskip detect   -set v0 -model ...  # transcripts -> predictions (model calls, paid)
//	adskip score    -run <id>           # predictions vs gold -> runs/<id>/score-*.json (offline)
//	adskip review   -file <tsv> -run .. # eye-check verdicts -> gold_reviews (offline)
//	adskip flags    -file <tsv>         # assessor video flags -> video_flags (offline)
//
// All private data (eval sets, subtitle cache, transcripts) lives under the data
// root (-data, default ~/Web/data), outside the repository.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bayanovea/native-ad-skip-eval/internal/provider"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := provider.LoadFile(envOr("ADSKIP_PROVIDERS", home(".config/native-ad-skip-eval/providers.json"))); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
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
	case "rank":
		err = runRank(os.Args[2:])
	case "paired":
		err = runPaired(os.Args[2:])
	case "ceiling":
		err = runCeiling(os.Args[2:])
	case "offset":
		err = runOffset(os.Args[2:])
	case "flags":
		err = runFlags(os.Args[2:])
	case "sheet":
		err = runSheet(os.Args[2:])
	case "compare":
		err = runCompare(os.Args[2:])
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
  adskip fetch    [-provider NAME] [-set NAME] [-ids FILE] [-data DIR] [-lang ru] [-force] [-retries N] [-yt-dlp PATH] [-proxy URL] [id ...]
  adskip collapse [-provider NAME] [-data DIR] [-lang ru] -id NATIVE_ID
  adskip import   [-data DIR] -file GOLD_JSONL
  adskip evalset  heldout | v0 [-data DIR] [-n 10] [-min-contributors 3] [-exclude c2-spike]
  adskip detect   -set NAME [-model claude-opus-5-5] [-effort medium] [-transport api|cli] [-tag NAME] [-prompt prompts/v2.2.md] [-limit N] [-dry-run] [-force] [-data DIR]
  adskip score    -run RUN_ID [-min-seconds 5] [-min-confidence low|medium|high] [-iou 0.5] [-gold all|assessor|reviewed] [-videos FILE] [-list] [-data DIR]
  adskip rank     [-exclude-owner OWNER] [-videos FILE] [-timing-clean] [-lambda 10] label=run[,run...] ...
  adskip paired   [-gold reviewed] [-videos FILE] [-timing-clean] [-lambda 10] [-boot 10000] BASE_RUN CANDIDATE_RUN
  adskip ceiling  [-min-contributors 2] [-set NAME]   # human-human ceiling in SCORE
  adskip offset   -id NATIVE_ID -seconds N -date YYYY-MM-DD [-note TEXT] [-data DIR]
  adskip flags    -file VIDEO_FLAGS_TSV [-data DIR]
  adskip sheet    -runs RUN1,RUN2,... -out PATH.md [-min-seconds 5] [-data DIR]
  adskip compare  -a RUN -b RUN [-iou 0.5] [-min-seconds 5] [-data DIR]
  adskip review   -file VERDICTS_TSV -run RUN_ID -date YYYY-MM-DD [-reviewer NAME] [-data DIR]`)
}

// providerFlag registers -provider; "" resolves to the only configured platform.
func providerFlag(fs *flag.FlagSet) *string {
	return fs.String("provider", "", "video platform (default: the only configured one)")
}

// resolveProvider replaces the flag value with the resolved name, so "" becomes
// the real name everywhere the command keys data by it.
func resolveProvider(name *string) (provider.Provider, error) {
	p, err := provider.Resolve(*name)
	if err != nil {
		return nil, err
	}
	*name = p.Name()
	return p, nil
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
