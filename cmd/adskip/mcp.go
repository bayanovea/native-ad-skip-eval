package main

// adskip mcp serves the eval results to an MCP host (Claude Code) over stdio.
// Two read-only tools: list_runs and get_run_metrics. stdout is the protocol
// stream, so nothing on this path may print to it; errors go to stderr.
//
//	claude mcp add adskip -- /path/to/adskip mcp -runs /path/to/native-ad-skip-eval/runs

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayanovea/native-ad-skip-eval/internal/score"
	"github.com/bayanovea/native-ad-skip-eval/internal/store"
)

type listRunsIn struct {
	Model  string `json:"model,omitempty" jsonschema:"substring of the model name, e.g. sonnet or luna"`
	Prompt string `json:"prompt,omitempty" jsonschema:"prompt version, e.g. v2.2"`
	Limit  int    `json:"limit,omitempty" jsonschema:"how many runs to return, newest first; default 10, max 50"`
}

type runMetricsIn struct {
	RunID string `json:"run_id" jsonschema:"run id exactly as list_runs returns it"`
}

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	data := fs.String("data", defaultDataDir(), "private data root")
	runsDir := fs.String("runs", "runs", "directory with run config and metrics")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "adskip", Version: "0.1.0"}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_runs",
		Description: "Use to find run ids before asking about a run. Returns runs newest first: id, date, model, effort, prompt version, eval set, transport.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listRunsIn) (*mcp.CallToolResult, any, error) {
		runs, err := readRuns(*runsDir)
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatRuns(filterRuns(runs, in), len(runs))), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_run_metrics",
		Description: "Use to see how a prompt or model run did. Returns SCORE (share of ad time removed minus 10x the share " +
			"of content wrongly removed; higher is better) with the share of ad time removed, precision, seconds of content " +
			"wrongly cut per hour of video and cost per video. Computed on the reviewed gold with the product's skip thresholds.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runMetricsIn) (*mcp.CallToolResult, any, error) {
		runs, err := readRuns(*runsDir)
		if err != nil {
			return nil, nil, err
		}
		cfg, ok := findRun(runs, in.RunID)
		if !ok {
			return nil, nil, fmt.Errorf("run %q not found; closest: %s", in.RunID, strings.Join(closestRuns(runs, in.RunID, 5), ", "))
		}
		text, err := headlineMetrics(ctx, st, *data, *runsDir, cfg)
		if err != nil {
			return nil, nil, err
		}
		return textResult(text), nil, nil
	})

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// readRuns loads every runs/<id>/config.json, newest first.
func readRuns(dir string) ([]runConfig, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "config.json"))
	if err != nil {
		return nil, err
	}
	var runs []runConfig
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var c runConfig
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if c.ID == "" {
			c.ID = filepath.Base(filepath.Dir(p))
		}
		runs = append(runs, c)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].CreatedAt != runs[j].CreatedAt {
			return runs[i].CreatedAt > runs[j].CreatedAt
		}
		return runs[i].ID > runs[j].ID
	})
	return runs, nil
}

func filterRuns(runs []runConfig, in listRunsIn) []runConfig {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)
	var out []runConfig
	for _, r := range runs {
		if in.Model != "" && !strings.Contains(strings.ToLower(r.Model), strings.ToLower(in.Model)) {
			continue
		}
		if in.Prompt != "" && !strings.EqualFold(r.PromptVersion, in.Prompt) {
			continue
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out
}

func formatRuns(runs []runConfig, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d runs (id · date · model/effort · prompt · eval set · transport)\n", len(runs), total)
	for _, r := range runs {
		fmt.Fprintf(&b, "%s · %s · %s/%s · %s · %s · %s\n", r.ID, dateOf(r.CreatedAt), r.Model, r.Effort, r.PromptVersion, r.Evalset, r.Transport)
	}
	return b.String()
}

func dateOf(ts string) string { return ts[:min(len(ts), 10)] }

func findRun(runs []runConfig, id string) (runConfig, bool) {
	for _, r := range runs {
		if r.ID == id {
			return r, true
		}
	}
	return runConfig{}, false
}

// closestRuns ranks run ids by how many dash-separated parts they share with
// the requested id; ties keep the newest-first order.
func closestRuns(runs []runConfig, id string, n int) []string {
	want := map[string]bool{}
	for _, p := range strings.Split(strings.ToLower(id), "-") {
		want[p] = true
	}
	type cand struct {
		id     string
		shared int
	}
	var cs []cand
	for _, r := range runs {
		k := 0
		for _, p := range strings.Split(strings.ToLower(r.ID), "-") {
			if want[p] {
				k++
			}
		}
		cs = append(cs, cand{r.ID, k})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].shared > cs[j].shared })
	var out []string
	for _, c := range cs[:min(n, len(cs))] {
		out = append(out, c.id)
	}
	return out
}

// headlineMetrics computes the headline of `adskip rank` live from the store, not
// from committed score files, which no longer all reproduce (docs/backlog).
func headlineMetrics(ctx context.Context, st *store.Store, data, runsDir string, cfg runConfig) (string, error) {
	p := scoreParams{Gold: "reviewed", DataRoot: data,
		Opt: score.Options{MinSeconds: 5, IoU: 0.3, MinConfidenceBy: productConfidence}}
	res, scored, reviews, err := computeScore(ctx, st, cfg.ID, p)
	if err != nil {
		return "", err
	}
	x := res.BySeconds[score.AnyAd]
	net := score.NetScore(x, 10)
	if scored == 0 || math.IsNaN(net) {
		return fmt.Sprintf("run %s: no scored videos with gold in the store", cfg.ID), nil
	}
	videos, err := selectVideos(ctx, st, cfg.ID, p)
	if err != nil {
		return "", err
	}
	hours := 0.0
	for _, v := range videos {
		hours += transcriptDuration(data, v[0], v[1]) / 3600
	}
	wrongPerHour := "n/a"
	if hours > 0 {
		wrongPerHour = fmt.Sprintf("%.1f s/h", x.WronglySkipped()/hours)
	}
	cost := "n/a"
	var m struct {
		CostPerVideo float64 `json:"cost_usd_per_ok_video"`
	}
	if b, err := os.ReadFile(filepath.Join(runsDir, cfg.ID, "metrics.json")); err == nil && json.Unmarshal(b, &m) == nil && m.CostPerVideo > 0 {
		cost = fmt.Sprintf("$%.4f", m.CostPerVideo)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "run %s (%s/%s, prompt %s, eval set %s)\n", cfg.ID, cfg.Model, cfg.Effort, cfg.PromptVersion, cfg.Evalset)
	fmt.Fprintf(&b, "SCORE               %.2f (1.0 = perfect, 0 = no detector, below 0 = worse than no detector)\n", net)
	fmt.Fprintf(&b, "ad time removed     %.0f%%\n", 100*x.Recall())
	fmt.Fprintf(&b, "precision (seconds) %.0f%%\n", 100*x.Precision())
	fmt.Fprintf(&b, "content wrongly cut %s\n", wrongPerHour)
	fmt.Fprintf(&b, "cost per video      %s\n", cost)
	fmt.Fprintf(&b, "videos scored       %d (%d review verdicts applied)\n", scored, reviews)
	fmt.Fprintf(&b, "settings            reviewed gold, product thresholds (sponsor >= medium, self-promotion high), IoU 0.3, segments >= 5 s, lambda 10\n")
	return b.String(), nil
}
