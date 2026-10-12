package detector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ClaudeCLI is the Client over `claude -p` (Claude Code in print mode), so a
// run is paid from the subscription quota instead of API dollars.
//
// It exists for experiments (many reruns, cost-lever comparisons). It is NOT
// the same instrument as the API client, and the README says so wherever a
// number comes from it:
//
//   - Structured output is delivered through a synthetic tool call
//     (--json-schema), not constrained decoding. The shape is not guaranteed
//     by the decoder; Validate still catches broken answers.
//   - Latency is wall time of a process with a two-turn exchange: not
//     comparable with API latency, never reported as such.
//   - Cost is the CLI's own accounting at list prices; we recompute it from
//     usage with the same price table as API runs, so the numbers stay
//     comparable, but nothing is billed.
//   - No temperature control; model and CLI behaviour change with releases,
//     so the CLI version goes into every run's config.
//
// The harness prompt is cut to the minimum with flags: our prompt replaces
// the default system prompt, no tools, no settings/CLAUDE.md/MCP/skills, no
// session files, an empty working directory. (--bare would do more but it
// refuses subscription auth.)
type ClaudeCLI struct {
	thinking bool // leave Claude Code's thinking on (default off, see Detect)
	model    string
	effort   string
	version  string
	workdir  string
}

// cliTimeout bounds one call; Opus on a 2-hour transcript is the slow case.
const cliTimeout = 15 * time.Minute

// NewClaudeCLI checks that the binary exists and builds a client. The working
// directory is a fresh empty temp dir, so nothing is auto-discovered from it.
func NewClaudeCLI(model, effort string) (*ClaudeCLI, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	out, err := exec.Command("claude", "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("claude binary: %w", err)
	}
	dir, err := os.MkdirTemp("", "adskip-claude-")
	if err != nil {
		return nil, err
	}
	return &ClaudeCLI{model: model, effort: effort, version: strings.TrimSpace(string(out)), workdir: dir}, nil
}

// WithThinking turns extended thinking on (it is off by default to match the
// API client). Output tokens grow by thousands per video: the cost is no longer
// input-only, which is exactly what the experiment measures.
func (c *ClaudeCLI) WithThinking() *ClaudeCLI { c.thinking = true; return c }

// Model implements Client.
func (c *ClaudeCLI) Model() string { return c.model }

// Version is the CLI version string, recorded in the run config.
func (c *ClaudeCLI) Version() string { return c.version }

// Close removes the temp working directory.
func (c *ClaudeCLI) Close() error { return os.RemoveAll(c.workdir) }

// cliResult is the subset of `--output-format json` we read.
type cliResult struct {
	IsError          bool            `json:"is_error"`
	Subtype          string          `json:"subtype"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	StopReason       string          `json:"stop_reason"`
	NumTurns         int             `json:"num_turns"`
	Usage            struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// Detect implements Client.
func (c *ClaudeCLI) Detect(ctx context.Context, req Request) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	args := []string{"-p",
		"--model", c.model,
		"--system-prompt", req.System,
		"--tools", "",
		"--setting-sources", "",
		"--disable-slash-commands",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--output-format", "json",
		"--json-schema", req.Schema,
	}
	if c.effort != "" && !strings.Contains(c.model, "haiku") {
		args = append(args, "--effort", c.effort)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = c.workdir
	cmd.Stdin = strings.NewReader(req.User)
	// Subscription auth only: an API key in the environment would silently
	// switch the call to API billing.
	cmd.Env = withoutEnv(os.Environ(), "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN")
	// The API client sends no thinking parameter, so the model does not think;
	// Claude Code turns thinking on by default (8k+ tokens, a minute per video).
	// Off, to measure the same thing.
	if !c.thinking {
		cmd.Env = append(cmd.Env, "CLAUDE_CODE_DISABLE_THINKING=1")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	runErr := cmd.Run()
	res := Result{Latency: time.Since(start)}

	var cr cliResult
	if err := json.Unmarshal(stdout.Bytes(), &cr); err != nil {
		if runErr != nil {
			return res, fmt.Errorf("claude -p: %v: %s", runErr, firstLine(stderr.String()))
		}
		return res, fmt.Errorf("claude -p: unreadable output: %w", err)
	}
	// Claude Code caches the request on its own (our client does not, D15).
	// Cache tokens are folded into plain input so the cost, computed from the
	// same price table, is the price of the same request without caching and
	// compares with API runs. The subscription is not billed per token anyway.
	res.Usage = Usage{
		InputTokens:  cr.Usage.InputTokens + cr.Usage.CacheReadInputTokens + cr.Usage.CacheCreationInputTokens,
		OutputTokens: cr.Usage.OutputTokens,
	}
	if cr.IsError {
		// e.g. "limit reached" messages arrive in result with is_error true.
		return res, fmt.Errorf("claude -p error (%s): %s", cr.Subtype, firstLine(cr.Result))
	}
	raw := string(cr.StructuredOutput)
	if raw == "" || raw == "null" {
		raw = cr.Result // fallback: the JSON text, if the CLI put it there
	}
	res.Raw = raw
	out, err := ParseOutput(raw)
	if err != nil {
		return res, err
	}
	res.Output = out
	return res, nil
}

func withoutEnv(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, n := range names {
			if strings.HasPrefix(kv, n+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
