package detector

import (
	"context"
	"errors"
)

// Anthropic is the Client over the Claude API (Go SDK). Structured output:
// the response format is the JSON schema in Request.Schema, so the response
// is one JSON content block that ParseOutput decodes.
//
// TODO(step 3, owner: user): implement with github.com/anthropics/anthropic-sdk-go.
//   - client: anthropic.NewClient() picks up ANTHROPIC_API_KEY or the `ant auth login` profile.
//   - request: Model, MaxTokens (small, ~2k: the output is a short JSON), System = req.System,
//     one user message = req.User, OutputConfig with Effort and the JSON-schema format.
//     Docs: https://platform.claude.com/docs/en/build-with-claude/structured-outputs.md
//     SDK:  https://github.com/anthropics/anthropic-sdk-go
//   - response: concatenate text blocks -> Raw; ParseOutput(Raw); Usage from response.Usage
//     (input, output, cache_read_input_tokens, cache_creation_input_tokens); Latency = wall clock
//     around the call; check StopReason before trusting the content (max_tokens, refusal).
//   - errors: typed SDK errors; 429/5xx are retried by the SDK (max_retries 2), do not wrap a loop
//     around it blindly. A refusal is an error for this video, not a crash of the run.
type Anthropic struct {
	model  string
	effort string
}

// NewAnthropic builds a client for one model and effort level.
func NewAnthropic(model, effort string) (*Anthropic, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	return &Anthropic{model: model, effort: effort}, nil
}

// Model implements Client.
func (a *Anthropic) Model() string { return a.model }

// Detect implements Client.
func (a *Anthropic) Detect(ctx context.Context, req Request) (Result, error) {
	return Result{}, errors.New("detector: Anthropic client not implemented yet (step 3 homework)")
}
