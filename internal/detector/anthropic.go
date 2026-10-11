package detector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Anthropic is the Client over the Claude API (Go SDK).
//
// One request per transcript: the prompt as the system text, the rendered
// cues as the only user message, the JSON schema as the response format
// (structured output), so the reply is one text block of schema-valid JSON.
//
// Deliberately not used: prompt caching (the prompt is below the cacheable
// minimum and nothing else repeats between calls), streaming (the output is
// a few hundred tokens), server-side model fallbacks (a fallback would run a
// different model under this run's name and corrupt the comparison).
type Anthropic struct {
	client anthropic.Client
	model  string
	effort string
}

// NewAnthropic builds a client for one model and effort level. Credentials
// come from the environment: ANTHROPIC_API_KEY or the `ant auth login` profile.
func NewAnthropic(model, effort string) (*Anthropic, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	return &Anthropic{client: anthropic.NewClient(), model: model, effort: effort}, nil
}

// Model implements Client.
func (a *Anthropic) Model() string { return a.model }

// maxTokens bounds the reply. The JSON is small, but on models with adaptive
// thinking the thinking tokens count against this limit too, so it is not
// set to the size of the JSON alone.
const maxTokens = 8192

// Detect implements Client.
func (a *Anthropic) Detect(ctx context.Context, req Request) (Result, error) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(req.Schema), &schema); err != nil {
		return Result{}, fmt.Errorf("schema: %w", err)
	}
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: req.System}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
	}
	// Effort exists on the current Opus/Sonnet generation only; Haiku 4.5 rejects it.
	if a.effort != "" && !strings.Contains(a.model, "haiku") {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(a.effort)
	}

	start := time.Now()
	msg, err := a.client.Messages.New(ctx, params)
	res := Result{Latency: time.Since(start)}
	if err != nil {
		return res, err // the SDK error text carries the HTTP status; detect.go reads it
	}
	res.Usage = Usage{
		InputTokens:      int(msg.Usage.InputTokens),
		OutputTokens:     int(msg.Usage.OutputTokens),
		CacheReadTokens:  int(msg.Usage.CacheReadInputTokens),
		CacheWriteTokens: int(msg.Usage.CacheCreationInputTokens),
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	res.Raw = text.String()

	// The schema guarantees the shape only when the model finished its turn.
	// A cut-off (max_tokens) or a refusal is a failed call for this video.
	switch msg.StopReason {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonStopSequence:
	case anthropic.StopReasonMaxTokens:
		return res, fmt.Errorf("stop_reason max_tokens: output cut off at %d tokens", maxTokens)
	default:
		return res, fmt.Errorf("stop_reason %s", msg.StopReason)
	}
	out, err := ParseOutput(res.Raw)
	if err != nil {
		return res, err
	}
	res.Output = out
	return res, nil
}
