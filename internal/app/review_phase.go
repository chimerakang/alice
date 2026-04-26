package app

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	appengine "claude-tg-agent/internal/app/engine"
)

// CLIReviewPhase runs the advisory reviewer through the configured client.
type CLIReviewPhase struct {
	client Client
	model  string
}

// NewCLIReviewPhase creates a reviewer that calls the client with a dedicated model.
func NewCLIReviewPhase(client Client, model string) *CLIReviewPhase {
	if client == nil {
		return nil
	}
	return &CLIReviewPhase{client: client, model: strings.TrimSpace(model)}
}

func (p *CLIReviewPhase) Review(ctx context.Context, req appengine.ReviewRequest) (appengine.ReviewResult, error) {
	if p == nil || p.client == nil {
		return appengine.ReviewResult{}, fmt.Errorf("review client is not configured")
	}

	var collected bytes.Buffer
	resp, err := p.client.CallPlan(ctx, appengine.BuildReviewPrompt(req), req.ProjectDir, p.model, func(contentType, text string) {
		if contentType == "text" {
			collected.WriteString(text)
		}
	})
	if err != nil {
		return appengine.ReviewResult{}, err
	}

	text := strings.TrimSpace(collected.String())
	if text == "" {
		text = strings.TrimSpace(resp.TextContent)
	}
	if text == "" {
		text = strings.TrimSpace(resp.Result)
	}

	review, err := appengine.ParseReviewResult(text)
	if err != nil {
		return appengine.ReviewResult{}, err
	}
	review.ReviewerModel = p.model
	review.InputTokens = resp.Usage.InputTokens
	review.OutputTokens = resp.Usage.OutputTokens
	review.CostUSD = resp.TotalCostUSD
	if review.ReviewerModel == "" {
		review.ReviewerModel = p.client.GetModel()
	}
	return review, nil
}
