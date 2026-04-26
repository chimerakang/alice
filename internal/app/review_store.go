package app

import (
	"context"
	"fmt"
	"strings"

	appengine "claude-tg-agent/internal/app/engine"
)

// StoreReview persists a completed Hermes review into the unified task schema.
func (s *SQLiteStorage) StoreReview(ctx context.Context, taskID string, review appengine.ReviewResult) error {
	_ = ctx

	if err := review.Validate(); err != nil {
		return fmt.Errorf("validate review: %w", err)
	}

	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        taskID,
		ReviewerModel: strings.TrimSpace(review.ReviewerModel),
		Verdict:       string(review.Verdict),
		OverallScore:  review.OverallScore,
		FeedbackText:  strings.TrimSpace(review.Feedback),
		IssueTags:     reviewTagsToStrings(review.IssueTags),
		InputTokens:   review.InputTokens,
		OutputTokens:  review.OutputTokens,
		CostUSD:       review.CostUSD,
	})
	if err != nil {
		return err
	}
	for _, subTask := range review.SubTaskResults {
		if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
			ReviewID:  reviewID,
			SubTaskID: subTask.SubTaskID,
			Score:     subTask.Score,
			Feedback:  strings.TrimSpace(subTask.Feedback),
			IssueTags: reviewTagsToStrings(subTask.IssueTags),
		}); err != nil {
			return err
		}
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
