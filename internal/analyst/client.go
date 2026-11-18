package analyst

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Request is one structured completion.
type Request struct {
	Model     string
	System    string
	User      string
	Schema    map[string]any
	MaxTokens int64
}

// Response is the model's JSON text and the usage the API reported.
type Response struct {
	Text         string
	InputTokens  int64
	OutputTokens int64
}

// Client is the language model. The production implementation calls the
// Anthropic API; tests use a fake returning canned JSON.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// requestTimeout bounds one model call (a long analysis can take minutes).
const requestTimeout = 6 * time.Minute

type anthropicClient struct{ c anthropic.Client }

// NewAnthropicClient builds the production client. The key is never logged.
func NewAnthropicClient(apiKey string) Client {
	return &anthropicClient{c: anthropic.NewClient(option.WithAPIKey(apiKey), option.WithRequestTimeout(requestTimeout))}
}

// Complete asks the model for JSON constrained by req.Schema (structured
// outputs, output_config.format). Forced tool use is not available on the
// current Sonnet, and structured outputs guarantee schema-valid JSON anyway.
func (a *anthropicClient) Complete(ctx context.Context, req Request) (Response, error) {
	msg, err := a.c.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: req.MaxTokens,
		System:    []anthropic.TextBlockParam{{Text: req.System}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	})
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			// Status and the API's own message only: the request (with its key
			// header and prompt) is never part of the error text.
			return Response{}, fmt.Errorf("anthropic API error %d: %s", apiErr.StatusCode, apiMessage(apiErr))
		}
		return Response{}, fmt.Errorf("anthropic request failed: %w", err)
	}
	usage := Response{
		InputTokens:  msg.Usage.InputTokens + msg.Usage.CacheCreationInputTokens + msg.Usage.CacheReadInputTokens,
		OutputTokens: msg.Usage.OutputTokens,
	}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return usage, fmt.Errorf("the model declined the request (refusal)")
	case anthropic.StopReasonMaxTokens:
		return usage, fmt.Errorf("the model ran out of output tokens before finishing")
	}
	for _, b := range msg.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			usage.Text += t.Text
		}
	}
	return usage, nil
}

func apiMessage(e *anthropic.Error) string {
	if m := e.Message; m != "" {
		return m
	}
	return "request rejected"
}
