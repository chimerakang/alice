package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// Verdict is the normalized overall review outcome.
type Verdict string

const (
	VerdictPass    Verdict = "pass"
	VerdictFail    Verdict = "fail"
	VerdictPartial Verdict = "partial"
)

// ReviewPhase evaluates a completed execution and returns advisory feedback.
type ReviewPhase interface {
	Review(ctx context.Context, req ReviewRequest) (ReviewResult, error)
}

// ReviewResultStore persists a completed review for later dashboard display.
type ReviewResultStore interface {
	StoreReview(ctx context.Context, taskID string, review ReviewResult) error
}

// ReviewRequest is the execution payload sent to a reviewer model.
type ReviewRequest struct {
	TaskID         string
	ProjectDir     string
	Goal           string
	Accumulated    string
	Plan           []hermes.SubTask
	SubTaskResults []ReviewSubTaskInput
	Artifacts      []Artifact
}

// ReviewSubTaskInput is the normalized sub-task payload for the reviewer.
type ReviewSubTaskInput struct {
	ID          string   `json:"id"`
	Index       int      `json:"index"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Result      string   `json:"result"`
	ToolHints   []string `json:"tool_hints,omitempty"`
}

// ReviewTag describes a categorized issue surfaced by the reviewer.
type ReviewTag string

const (
	ReviewTagAmbiguousGoal       ReviewTag = "ambiguous_goal"
	ReviewTagMissingContext      ReviewTag = "missing_context"
	ReviewTagWrongToolHint       ReviewTag = "wrong_tool_hint"
	ReviewTagUnderspecifiedInput ReviewTag = "underspecified_input"
	ReviewTagMissingValidation   ReviewTag = "missing_validation"
)

// ReviewSubTaskResult captures reviewer feedback for one sub-task.
type ReviewSubTaskResult struct {
	SubTaskID string      `json:"sub_task_id"`
	Score     int         `json:"score"`
	Feedback  string      `json:"feedback"`
	IssueTags []ReviewTag `json:"issue_tags"`
}

// ReviewResult is the normalized output returned by ReviewPhase.
type ReviewResult struct {
	ReviewerModel  string                `json:"reviewer_model,omitempty"`
	Verdict        Verdict               `json:"verdict"`
	OverallScore   int                   `json:"overall_score"`
	Feedback       string                `json:"feedback"`
	IssueTags      []ReviewTag           `json:"issue_tags"`
	SubTaskResults []ReviewSubTaskResult `json:"sub_task_results"`
	InputTokens    int                   `json:"input_tokens,omitempty"`
	OutputTokens   int                   `json:"output_tokens,omitempty"`
	CostUSD        float64               `json:"cost_usd,omitempty"`
}

// BuildReviewPrompt assembles the first-version reviewer prompt for #119.
func BuildReviewPrompt(req ReviewRequest) string {
	type reviewPromptArtifact struct {
		Path      string `json:"path"`
		Hash      string `json:"hash,omitempty"`
		SubTaskID string `json:"sub_task_id,omitempty"`
	}

	type reviewPromptPayload struct {
		TaskID         string                 `json:"task_id,omitempty"`
		ProjectDir     string                 `json:"project_dir,omitempty"`
		Goal           string                 `json:"goal"`
		Accumulated    string                 `json:"accumulated,omitempty"`
		Plan           []hermes.SubTask       `json:"plan"`
		SubTaskResults []ReviewSubTaskInput   `json:"sub_task_results"`
		Artifacts      []reviewPromptArtifact `json:"artifacts,omitempty"`
	}

	payload := reviewPromptPayload{
		TaskID:         strings.TrimSpace(req.TaskID),
		ProjectDir:     strings.TrimSpace(req.ProjectDir),
		Goal:           strings.TrimSpace(req.Goal),
		Accumulated:    strings.TrimSpace(req.Accumulated),
		Plan:           req.Plan,
		SubTaskResults: req.SubTaskResults,
	}
	if len(req.Artifacts) > 0 {
		payload.Artifacts = make([]reviewPromptArtifact, 0, len(req.Artifacts))
		for _, artifact := range req.Artifacts {
			payload.Artifacts = append(payload.Artifacts, reviewPromptArtifact{
				Path:      artifact.Path,
				Hash:      artifact.Hash,
				SubTaskID: artifact.SubTaskID,
			})
		}
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		body = []byte("{}")
	}

	var b strings.Builder
	b.WriteString("You are the ReviewPhase reviewer for a Hermes execution pipeline.\n")
	b.WriteString("Evaluate the completed task conservatively. Your output is advisory, but it must be concrete and evidence-based.\n")
	b.WriteString("Return exactly one JSON object with this shape:\n")
	b.WriteString("{\"verdict\":\"pass|partial|fail\",\"overall_score\":0,\"feedback\":\"...\",\"issue_tags\":[\"...\"],\"sub_task_results\":[{\"sub_task_id\":\"...\",\"score\":0,\"feedback\":\"...\",\"issue_tags\":[\"...\"]}]}\n")
	b.WriteString("Rules:\n")
	b.WriteString("- overall_score and every sub-task score must be integers from 0 to 100.\n")
	b.WriteString("- verdict must be one of pass, partial, fail.\n")
	b.WriteString("- issue_tags must be JSON arrays of snake_case labels such as ambiguous_goal, missing_context, wrong_tool_hint, underspecified_input, missing_validation.\n")
	b.WriteString("- Give specific feedback tied to the goal, plan, sub-task result quality, and artifacts.\n")
	b.WriteString("- Mark pass only when the goal appears satisfied and no material risks remain.\n")
	b.WriteString("- If context is insufficient, say so explicitly and add the relevant issue_tags.\n")
	b.WriteString("- Do not wrap the JSON in Markdown fences.\n\n")
	b.WriteString("Execution payload:\n")
	b.Write(body)
	b.WriteString("\n\n")
	b.WriteString("Review the execution now and return JSON only.")
	return b.String()
}

// ReviewInputsFromPlan normalizes planner sub-tasks plus their final results.
func ReviewInputsFromPlan(plan []hermes.SubTask) []ReviewSubTaskInput {
	out := make([]ReviewSubTaskInput, 0, len(plan))
	for idx, subTask := range plan {
		out = append(out, ReviewSubTaskInput{
			ID:          subTask.ID,
			Index:       idx,
			Description: subTask.Description,
			Status:      string(subTask.Status),
			Result:      strings.TrimSpace(subTask.Result),
			ToolHints:   append([]string(nil), subTask.ToolHints...),
		})
	}
	return out
}

func (v Verdict) Valid() bool {
	switch v {
	case VerdictPass, VerdictFail, VerdictPartial:
		return true
	default:
		return false
	}
}

func (r ReviewResult) Validate() error {
	if !r.Verdict.Valid() {
		return fmt.Errorf("invalid verdict %q", r.Verdict)
	}
	if r.OverallScore < 0 || r.OverallScore > 100 {
		return fmt.Errorf("overall_score out of range: %d", r.OverallScore)
	}
	for _, subTask := range r.SubTaskResults {
		if subTask.Score < 0 || subTask.Score > 100 {
			return fmt.Errorf("sub-task %q score out of range: %d", subTask.SubTaskID, subTask.Score)
		}
	}
	return nil
}

var reviewJSONBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*?\\})\\s*```")

// ParseReviewResult decodes reviewer JSON from either raw text or a fenced code block.
func ParseReviewResult(text string) (ReviewResult, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return ReviewResult{}, fmt.Errorf("review output is empty")
	}
	if m := reviewJSONBlockRe.FindStringSubmatch(raw); len(m) > 1 {
		raw = strings.TrimSpace(m[1])
	}
	var result ReviewResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return ReviewResult{}, err
	}
	if err := result.Validate(); err != nil {
		return ReviewResult{}, err
	}
	return result, nil
}
