package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

// StoreReview persists a completed Hermes review into the unified task schema.
func (s *SQLiteStorage) StoreReview(ctx context.Context, taskID string, review appengine.ReviewResult) error {
	return s.StoreReviewWithSource(ctx, taskID, review, "initial")
}

// StoreReviewWithSource persists a review and labels whether it came from the
// initial ReviewPhase pass or a manual/automatic retry.
func (s *SQLiteStorage) StoreReviewWithSource(ctx context.Context, taskID string, review appengine.ReviewResult, source string) error {
	if err := review.Validate(); err != nil {
		return fmt.Errorf("validate review: %w", err)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "initial"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review transaction: %w", err)
	}
	defer tx.Rollback()

	reviewID, err := insertUnifiedReviewResultTx(tx, UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: strings.TrimSpace(review.ReviewerModel),
		Verdict:       string(review.Verdict),
		OverallScore:  review.OverallScore,
		FeedbackText:  strings.TrimSpace(review.Feedback),
		IssueTags:     reviewTagsToStrings(review.IssueTags),
		InputTokens:   review.InputTokens,
		OutputTokens:  review.OutputTokens,
		CostUSD:       review.CostUSD,
		Source:        source,
	})
	if err != nil {
		return err
	}
	for _, subTask := range review.SubTaskResults {
		if err := insertUnifiedReviewSubTaskResultTx(tx, UnifiedReviewSubTaskResult{
			ReviewID:  reviewID,
			SubTaskID: subTask.SubTaskID,
			Score:     subTask.Score,
			Feedback:  strings.TrimSpace(subTask.Feedback),
			IssueTags: reviewTagsToStrings(subTask.IssueTags),
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit review transaction: %w", err)
	}
	s.broadcastUnifiedTask(taskID)
	return nil
}

func reviewTagsToStrings(tags []appengine.ReviewTag) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, string(tag))
	}
	return out
}

func insertUnifiedReviewResultTx(tx *sql.Tx, review UnifiedReviewResult) (int64, error) {
	if review.CreatedAt.IsZero() {
		review.CreatedAt = time.Now()
	}
	if strings.TrimSpace(review.Source) == "" {
		review.Source = "initial"
	}
	tagsJSON, err := json.Marshal(review.IssueTags)
	if err != nil {
		return 0, err
	}
	res, err := tx.Exec(`
		INSERT INTO review_results
			(task_id, reviewer_model, verdict, overall_score, feedback_text, issue_tags,
			 input_tokens, output_tokens, cost_usd, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		review.TaskID, review.ReviewerModel, review.Verdict, review.OverallScore,
		review.FeedbackText, string(tagsJSON), review.InputTokens, review.OutputTokens,
		review.CostUSD, review.Source, review.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func insertUnifiedReviewSubTaskResultTx(tx *sql.Tx, result UnifiedReviewSubTaskResult) error {
	tagsJSON, err := json.Marshal(result.IssueTags)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO review_subtask_results
			(review_id, sub_task_id, score, feedback, issue_tags)
		VALUES (?, ?, ?, ?, ?)`,
		result.ReviewID, result.SubTaskID, result.Score, result.Feedback, string(tagsJSON),
	)
	return err
}
