package app

import (
	"context"
	"strings"
	"testing"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
)

type recordingReviewClient struct {
	prompt        string
	projectDir    string
	modelOverride string
	callbackText  string
	response      *CLIResponse
}

func (c *recordingReviewClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *recordingReviewClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	return nil, nil
}

func (c *recordingReviewClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	c.prompt = message
	c.projectDir = projectDir
	c.modelOverride = modelOverride
	if onContent != nil && c.callbackText != "" {
		onContent("text", c.callbackText)
	}
	if c.response != nil {
		return c.response, nil
	}
	return &CLIResponse{
		Result:      "ignored",
		TextContent: "",
	}, nil
}

func (c *recordingReviewClient) GetModel() string {
	return "fallback-review-model"
}

func TestCLIReviewPhaseUsesPromptAndParsesJSONOnly(t *testing.T) {
	client := &recordingReviewClient{
		callbackText: `{"verdict":"pass","overall_score":92,"feedback":"ok","issue_tags":["missing_validation"],"sub_task_results":[{"sub_task_id":"s1","score":92,"feedback":"good","issue_tags":["missing_validation"]}]}`,
		response: &CLIResponse{
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 13, OutputTokens: 7},
			TotalCostUSD: 0.44,
		},
	}

	phase := NewCLIReviewPhase(client, "gpt-5.5")
	review, err := phase.Review(context.Background(), appengine.ReviewRequest{
		TaskID:     "task-review",
		ProjectDir: "/repo",
		Goal:       "修正 review 迴路",
		Plan: []hermes.SubTask{
			{ID: "s1", Description: "檢查結果", Status: hermes.SubTaskDone},
		},
		SubTaskResults: []appengine.ReviewSubTaskInput{
			{ID: "s1", Index: 0, Description: "檢查結果", Status: "done", Result: "ok"},
		},
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if client.projectDir != "/repo" || client.modelOverride != "gpt-5.5" {
		t.Fatalf("client call args = %+v", client)
	}
	if !strings.Contains(client.prompt, "ReviewPhase reviewer") || !strings.Contains(client.prompt, `"task_id": "task-review"`) {
		t.Fatalf("prompt missing review payload:\n%s", client.prompt)
	}
	if review.ReviewerModel != "gpt-5.5" || review.InputTokens != 13 || review.OutputTokens != 7 || review.CostUSD != 0.44 {
		t.Fatalf("unexpected review metadata: %+v", review)
	}
	if review.Verdict != appengine.VerdictPass || len(review.SubTaskResults) != 1 {
		t.Fatalf("unexpected review result: %+v", review)
	}
}

func TestCLIReviewPhaseFallsBackToClientModel(t *testing.T) {
	phase := NewCLIReviewPhase(&recordingReviewClient{
		callbackText: `{"verdict":"partial","overall_score":80,"feedback":"ok","issue_tags":[],"sub_task_results":[]}`,
	}, "")
	review, err := phase.Review(context.Background(), appengine.ReviewRequest{
		Goal: "測試 fallback",
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if review.ReviewerModel != "fallback-review-model" {
		t.Fatalf("reviewer model = %q, want fallback client model", review.ReviewerModel)
	}
}
