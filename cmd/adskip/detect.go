package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ad-skip-segment-eval/internal/detector"
	"ad-skip-segment-eval/internal/store"
)

// runConfig is runs/<id>/config.json: everything needed to repeat the run.
// Prices are copied in because the price list changes and the cost must not.
type runConfig struct {
	ID            string         `json:"id"`
	CreatedAt     string         `json:"created_at"`
	Model         string         `json:"model"`
	Effort        string         `json:"effort"`
	Thinking      bool           `json:"thinking,omitempty"`
	NoTimes       bool           `json:"no_times,omitempty"`
	WindowSeconds float64        `json:"window_seconds,omitempty"`
	Transport     string         `json:"transport"`              // api | cli | batch
	PriceFactor   float64        `json:"price_factor,omitempty"` // 0.5 for batch: the Batches API bills half
	BatchID       string         `json:"batch_id,omitempty"`
	CLIVersion    string         `json:"cli_version,omitempty"` // transport=cli only: the instrument changes with releases
	PromptVersion string         `json:"prompt_version"`
	PromptSHA     string         `json:"prompt_sha256"`
	Evalset       string         `json:"evalset"`
	Provider      string         `json:"provider"`
	Prices        detector.Price `json:"prices_usd_per_mtok"`
	// MinSegmentSeconds is not applied here: predictions are stored raw and the
	// scorer filters, so one run can be scored under several thresholds.
	MinSegmentSeconds float64 `json:"min_segment_seconds_for_scoring"`
}

// runMetrics is runs/<id>/metrics.json: aggregates only, never per video.
type runMetrics struct {
	Videos        int            `json:"videos"`
	Status        map[string]int `json:"status"` // ok | no_transcript | api_error | invalid_output
	Segments      map[string]int `json:"segments_by_category"`
	Confidence    map[string]int `json:"segments_by_confidence"`
	QuoteMismatch int            `json:"segments_quote_mismatch"`
	VideosWithAds int            `json:"videos_with_segments"`
	Tokens        struct {
		Input, Output, CacheRead, CacheWrite int
	} `json:"tokens"`
	CostUSD      float64                     `json:"cost_usd"`
	CostPerVideo float64                     `json:"cost_usd_per_ok_video"`
	LatencyMS    struct{ P50, P95, Max int } `json:"latency_ms"`
}

// runDetect runs the detector over an eval set and records one run.
//
//	adskip detect -set v0 -model claude-opus-5-5 -effort medium [-prompt prompts/v1.md]
//
// Per-video output (calls, predictions) goes to adskip.db; config, prompt copy
// and aggregate metrics go to runs/<date>-<model>-<prompt version>/ in the repo.
// Re-running the same id resumes: videos that already have a call are skipped,
// so an interrupted run never repeats a paid call. -force starts over.
func runDetect(args []string) error {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	providerName := fs.String("provider", "main", "video platform")
	data := fs.String("data", defaultDataDir(), "private data root")
	set := fs.String("set", "", "eval set name under <data>/evalsets (required)")
	model := fs.String("model", "claude-opus-5-5", "model id")
	effort := fs.String("effort", "medium", "effort level: low | medium | high")
	transport := fs.String("transport", "api", "api (Messages API, billed) | batch (Message Batches API, billed at half, asynchronous) | cli (`claude -p`, subscription quota; not the same instrument, see README)")
	batchID := fs.String("batch-id", "", "transport batch: attach to an already submitted batch instead of submitting again")
	noTimes := fs.Bool("no-times", false, "request format: omit cue times (needs a prompt that describes this format)")
	window := fs.Float64("window", 0, "request format: merge cues into windows of at least this many seconds (0 = off)")
	thinking := fs.Bool("thinking", false, "thinking on: cli keeps it on (off by default there), api/batch send thinking {type: adaptive} (the API default is no thinking output)")
	tag := fs.String("tag", "", "suffix for the run id, e.g. \"rerun\"; a different tag is a different run")
	promptPath := fs.String("prompt", "prompts/v1.md", "prompt file; its name is the prompt version")
	runsDir := fs.String("runs", "runs", "directory for run config and metrics")
	limit := fs.Int("limit", 0, "stop after N videos (0 = all); for a cheap first try")
	dryRun := fs.Bool("dry-run", false, "render requests and estimate size, no model calls, nothing written")
	force := fs.Bool("force", false, "replace an existing run with the same id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *set == "" {
		return errors.New("-set is required")
	}
	ids, err := readIDs(evalsetPath(*data, *set))
	if err != nil {
		return err
	}
	if *limit > 0 && *limit < len(ids) {
		ids = ids[:*limit]
	}
	prompt, err := detector.LoadPrompt(*promptPath)
	if err != nil {
		return err
	}
	price, ok := detector.Prices[*model]
	if !ok {
		fmt.Fprintf(os.Stderr, "warning: no price for %s, cost will be 0\n", *model)
	}
	if *transport != "api" && *transport != "cli" && *transport != "batch" {
		return fmt.Errorf("-transport must be api, cli or batch, got %q", *transport)
	}
	priceFactor := 1.0
	if *transport == "batch" {
		priceFactor = 0.5
	}
	runID := fmt.Sprintf("%s-%s-%s", time.Now().UTC().Format("2006-01-02"), *model, prompt.Version)
	if *transport == "cli" || *transport == "batch" {
		runID += "-" + *transport
	}
	format := detector.Format{NoTimes: *noTimes, WindowSeconds: *window}
	if format.NoTimes {
		runID += "-nt"
	}
	if *thinking {
		runID += "-think"
	}
	if format.WindowSeconds > 0 {
		runID += fmt.Sprintf("-w%g", format.WindowSeconds)
	}
	if *tag != "" {
		runID += "-" + *tag
	}
	cfg := runConfig{
		ID: runID, CreatedAt: time.Now().UTC().Format(time.RFC3339), Model: *model, Effort: *effort, Transport: *transport, Thinking: *thinking, PriceFactor: priceFactor, NoTimes: format.NoTimes, WindowSeconds: format.WindowSeconds,
		PromptVersion: prompt.Version, PromptSHA: prompt.SHA, Evalset: *set, Provider: *providerName,
		Prices: price, MinSegmentSeconds: 5,
	}

	if *dryRun {
		return dryRunDetect(cfg, prompt, format, cacheDir(*data, *providerName), ids)
	}

	var client detector.Client
	var batchClient *detector.AnthropicBatch
	switch *transport {
	case "cli":
		c, err := detector.NewClaudeCLI(*model, *effort)
		if err != nil {
			return err
		}
		defer c.Close()
		if *thinking {
			c.WithThinking()
		}
		cfg.CLIVersion = c.Version()
		client = c
	case "batch":
		c, err := detector.NewAnthropicBatch(*model, *effort)
		if err != nil {
			return err
		}
		if *thinking {
			c.WithThinking()
		}
		client, batchClient = c, c
	default:
		c, err := detector.NewAnthropic(*model, *effort)
		if err != nil {
			return err
		}
		if *thinking {
			c.WithThinking()
		}
		client = c
	}
	st, err := store.Open(store.Path(*data))
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	cfgJSON, _ := json.MarshalIndent(cfg, "", "  ")
	run := store.Run{ID: runID, CreatedAt: cfg.CreatedAt, Model: *model, Effort: *effort, PromptVersion: prompt.Version,
		PromptSHA: prompt.SHA, Evalset: *set, ConfigJSON: string(cfgJSON)}
	if err := st.CreateRun(ctx, run, *force); err != nil {
		if !*force && !resumable(ctx, st, runID) {
			return err
		}
		fmt.Fprintf(os.Stderr, "resuming run %s\n", runID)
	}

	if batchClient != nil {
		// Build the requests of every video that still needs a call, submit them
		// once (or attach to -batch-id), wait, and let the loop below read the results.
		var pending []detector.Request
		for _, id := range ids {
			if status, err := st.CallStatus(ctx, runID, *providerName, id); err != nil {
				return err
			} else if status == "ok" || status == "no_transcript" {
				continue
			}
			meta, cues, err := readTranscript(filepath.Join(cacheDir(*data, *providerName), id+".jsonl"))
			if err != nil || len(cues) == 0 {
				continue
			}
			req := detector.NewRequestFormat(prompt, meta, format.Apply(cues), format)
			req.Key = id
			pending = append(pending, req)
		}
		if len(pending) > 0 {
			bid := *batchID
			if bid == "" {
				if bid, err = batchClient.Submit(ctx, pending); err != nil {
					return fmt.Errorf("submit batch: %w", err)
				}
				fmt.Printf("batch submitted: %s (%d requests); re-attach with -batch-id %s if interrupted\n", bid, len(pending), bid)
			}
			cfg.BatchID = bid
			if err := batchClient.Wait(ctx, bid, 30*time.Second, func(line string) { fmt.Println(line) }); err != nil {
				return fmt.Errorf("wait for batch: %w", err)
			}
		}
	}

	m := runMetrics{Status: map[string]int{}, Segments: map[string]int{}, Confidence: map[string]int{}}
	var latencies []int
	for i, id := range ids {
		if status, err := st.CallStatus(ctx, runID, *providerName, id); err != nil {
			return err
		} else if status == "ok" || status == "no_transcript" {
			fmt.Printf("[%d/%d] %s: already %s, skipped\n", i+1, len(ids), id, status)
			continue
		}
		call := store.Call{RunID: runID, Provider: *providerName, NativeID: id}
		var preds []store.Prediction
		meta, cues, err := readTranscript(filepath.Join(cacheDir(*data, *providerName), id+".jsonl"))
		cues = format.Apply(cues)
		switch {
		case err != nil || len(cues) == 0:
			call.Status = "no_transcript"
			if err != nil {
				call.Error = err.Error()
			}
		default:
			res, err := client.Detect(ctx, withKey(detector.NewRequestFormat(prompt, meta, cues, format), id))
			call.InputTokens, call.OutputTokens = res.Usage.InputTokens, res.Usage.OutputTokens
			call.CacheReadTokens, call.CacheWriteTokens = res.Usage.CacheReadTokens, res.Usage.CacheWriteTokens
			call.LatencyMS = int(res.Latency / time.Millisecond)
			call.CostUSD = price.Cost(res.Usage) * priceFactor
			switch {
			case err != nil:
				call.Status, call.Error = "api_error", err.Error()
			case detector.Validate(cues, res.Output) != nil:
				call.Status, call.Error = "invalid_output", detector.Validate(cues, res.Output).Error()
			default:
				call.Status = "ok"
				for _, p := range detector.Resolve(cues, res.Output) {
					preds = append(preds, store.Prediction{RunID: runID, Provider: *providerName, NativeID: id,
						Category: p.Category, Start: p.Start, End: p.End, FirstCue: p.FirstCue, LastCue: p.LastCue,
						Confidence: p.Confidence, Quote: p.Quote, QuoteOK: detector.QuoteMatches(cues, p.Segment)})
				}
			}
		}
		if err := st.SaveCall(ctx, call, preds); err != nil {
			return err
		}
		fmt.Printf("[%d/%d] %s: %s", i+1, len(ids), id, call.Status)
		if call.Status == "ok" {
			fmt.Printf(", %d segments, %d in tokens, %.1fs, $%.4f", len(preds), call.InputTokens, float64(call.LatencyMS)/1000, call.CostUSD)
		} else if call.Error != "" {
			fmt.Printf(": %s", call.Error)
		}
		fmt.Println()
		if call.Status == "api_error" && isFatal(call.Error) {
			return fmt.Errorf("stopping the run: %s", call.Error)
		}
	}

	// Aggregate from the store, so a resumed run reports all of its videos.
	if err := aggregate(ctx, st, runID, &m, &latencies); err != nil {
		return err
	}
	dir := filepath.Join(*runsDir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cfgJSON, _ = json.MarshalIndent(cfg, "", "  ") // re-marshal: the batch id is known only now
	if err := os.WriteFile(filepath.Join(dir, "config.json"), cfgJSON, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(prompt.Text), 0o644); err != nil {
		return err
	}
	mJSON, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "metrics.json"), mJSON, 0o644); err != nil {
		return err
	}
	fmt.Printf("\nrun %s: %d videos, status %v, segments %v, $%.2f total -> %s\n", runID, m.Videos, m.Status, m.Segments, m.CostUSD, dir)
	return nil
}

// dryRunDetect prints what a run would send: per-video cue and character
// counts and a rough token estimate (Cyrillic runs ~2 chars per token on the
// current tokenizer; count_tokens gives the exact number before a paid run).
func dryRunDetect(cfg runConfig, prompt detector.Prompt, format detector.Format, dir string, ids []string) error {
	var chars, cues, missing int
	for _, id := range ids {
		meta, c, err := readTranscript(filepath.Join(dir, id+".jsonl"))
		if err != nil || len(c) == 0 {
			missing++
			fmt.Printf("%-24s no transcript\n", id)
			continue
		}
		c = format.Apply(c)
		req := detector.NewRequestFormat(prompt, meta, c, format)
		chars += len(req.User)
		cues += len(c)
		fmt.Printf("%-24s %5d cues %7d chars  %s\n", id, len(c), len(req.User), meta.TrackType)
	}
	est := (chars + len(ids)*len(prompt.Text)) / 2
	fmt.Printf("\n%s: %d videos (%d without transcript), %d cues, %d chars -> ~%dk input tokens, ~$%.2f on %s\n",
		cfg.ID, len(ids), missing, cues, chars, est/1000, float64(est)*cfg.Prices.Input/1e6, cfg.Model)
	return nil
}

func resumable(ctx context.Context, st *store.Store, runID string) bool {
	var n int
	return st.DB.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE id = ?`, runID).Scan(&n) == nil && n == 1
}

// isFatal: errors that will repeat on every video (auth, billing, bad request)
// stop the run; per-video errors (timeouts, refusals) do not.
func isFatal(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{"401", "403", "authentication", "credit", "billing", "400", "invalid_request", "not implemented", "limit reached", "usage limit"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

func aggregate(ctx context.Context, st *store.Store, runID string, m *runMetrics, latencies *[]int) error {
	rows, err := st.DB.QueryContext(ctx, `SELECT status, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, latency_ms, cost_usd
		FROM calls WHERE run_id = ?`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var in, out, cr, cw, lat int
		var cost float64
		if err := rows.Scan(&status, &in, &out, &cr, &cw, &lat, &cost); err != nil {
			return err
		}
		m.Videos++
		m.Status[status]++
		m.Tokens.Input += in
		m.Tokens.Output += out
		m.Tokens.CacheRead += cr
		m.Tokens.CacheWrite += cw
		m.CostUSD += cost
		if status == "ok" {
			*latencies = append(*latencies, lat)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n := len(*latencies); n > 0 {
		sort.Ints(*latencies)
		m.LatencyMS.P50 = (*latencies)[n/2]
		m.LatencyMS.P95 = (*latencies)[(n*95)/100]
		m.LatencyMS.Max = (*latencies)[n-1]
		m.CostPerVideo = m.CostUSD / float64(n)
	}
	prow, err := st.DB.QueryContext(ctx, `SELECT category, confidence, quote_ok, native_id FROM predictions WHERE run_id = ?`, runID)
	if err != nil {
		return err
	}
	defer prow.Close()
	videos := map[string]bool{}
	for prow.Next() {
		var cat, conf, id string
		var qok bool
		if err := prow.Scan(&cat, &conf, &qok, &id); err != nil {
			return err
		}
		m.Segments[cat]++
		m.Confidence[conf]++
		if !qok {
			m.QuoteMismatch++
		}
		videos[id] = true
	}
	m.VideosWithAds = len(videos)
	return prow.Err()
}

func withKey(r detector.Request, key string) detector.Request {
	r.Key = key
	return r
}
