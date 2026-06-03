package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
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
		TaskID:         taskID,
		ReviewerModel:  strings.TrimSpace(review.ReviewerModel),
		Verdict:        string(review.Verdict),
		OverallScore:   review.OverallScore,
		FeedbackText:   strings.TrimSpace(review.Feedback),
		IssueTags:      reviewTagsToStrings(review.IssueTags),
		InputTokens:    review.InputTokens,
		OutputTokens:   review.OutputTokens,
		CostUSD:        review.CostUSD,
		BlockCount:     review.BlockCount,
		AutoFixedCount: review.AutoFixedCount,
		Source:         source,
	})
	if err != nil {
		return err
	}
	subTaskIDs, err := reviewSubTaskStorageIDsTx(ctx, tx, taskID, review.SubTaskResults)
	if err != nil {
		return err
	}
	for idx, subTask := range review.SubTaskResults {
		if err := insertUnifiedReviewSubTaskResultTx(tx, UnifiedReviewSubTaskResult{
			ReviewID:  reviewID,
			SubTaskID: subTaskIDs[idx],
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

func reviewSubTaskStorageIDsTx(ctx context.Context, tx *sql.Tx, taskID string, results []appengine.ReviewSubTaskResult) ([]string, error) {
	if len(results) == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, idx
		FROM sub_tasks
		WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[string]string)
	byIndex := make(map[int]string)
	for rows.Next() {
		var id string
		var idx int
		if err := rows.Scan(&id, &idx); err != nil {
			return nil, err
		}
		byID[id] = id
		if suffix := strings.TrimPrefix(id, taskID+":"); suffix != id {
			byID[suffix] = id
		}
		byIndex[idx] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]string, len(results))
	for idx, subTask := range results {
		out[idx] = reviewSubTaskStorageID(taskID, idx, subTask.SubTaskID)
		raw := strings.TrimSpace(subTask.SubTaskID)
		if id, ok := byID[out[idx]]; ok {
			out[idx] = id
			continue
		}
		if id, ok := byID[raw]; ok {
			out[idx] = id
			continue
		}
		if displayIdx, ok := reviewSubTaskDisplayIndexFromID(raw); ok {
			if id, ok := byIndex[displayIdx-1]; ok {
				out[idx] = id
				continue
			}
		}
		if id, ok := byIndex[idx]; raw == "" && ok {
			out[idx] = id
		}
	}
	return out, nil
}

func reviewSubTaskDisplayIndexFromID(subTaskID string) (int, bool) {
	value := strings.TrimSpace(subTaskID)
	if value == "" {
		return 0, false
	}
	if colon := strings.LastIndex(value, ":"); colon >= 0 {
		value = value[colon+1:]
	}
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "s"), "S")
	if idx, err := strconv.Atoi(value); err == nil && idx > 0 {
		return idx, true
	}
	return 0, false
}

func reviewSubTaskStorageID(taskID string, idx int, subTaskID string) string {
	subTaskID = strings.TrimSpace(subTaskID)
	if strings.HasPrefix(subTaskID, taskID+":") {
		return subTaskID
	}
	return hermes.UnifiedSubTaskID(taskID, idx, subTaskID)
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
			 input_tokens, output_tokens, cost_usd, block_count, auto_fixed_count, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		review.TaskID, review.ReviewerModel, review.Verdict, review.OverallScore,
		review.FeedbackText, string(tagsJSON), review.InputTokens, review.OutputTokens,
		review.CostUSD, review.BlockCount, review.AutoFixedCount, review.Source,
		review.CreatedAt.Format(time.RFC3339Nano),
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
