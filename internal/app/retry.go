package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
)

const (
	maxSubTaskRetryAttempts = 3
	retrySelectionTimeout   = 30 * time.Second
)

type retrySelection struct {
	Task              UnifiedTask
	SubTask           UnifiedSubTask
	Review            UnifiedReviewResult
	SubTaskReview     UnifiedReviewSubTaskResult
	RetryCount        int
	DisplaySubTaskIdx int
	PreferCodex       bool
}

type retryOutcome struct {
	Selection retrySelection
	Result    appengine.Result
	Review    appengine.ReviewResult
	Improved  bool
	Duration  time.Duration
}

type retryTaskCandidate struct {
	ID                string
	Goal              string
	GithubIssueNumber int
	FailedCount       int
	LatestReviewAt    time.Time
}

func retrySelectionNeedsRetry(selection retrySelection) bool {
	verdict := strings.ToLower(strings.TrimSpace(selection.Review.Verdict))
	return verdict == "partial" || verdict == "fail" || selection.SubTaskReview.Score < 70
}

func decideSubTaskRetryRecovery(selection retrySelection) appengine.RecoveryDecision {
	return appengine.DecideRecovery(appengine.RecoveryRequest{
		Mode:        "sub_task_retry",
		Attempt:     selection.RetryCount,
		MaxAttempts: maxSubTaskRetryAttempts,
	})
}

func formatRetryNoRetryNeeded(selection retrySelection) string {
	verdict := strings.TrimSpace(selection.Review.Verdict)
	if verdict == "" {
		verdict = "unknown"
	}
	return fmt.Sprintf("✅ Sub-task #%d 評分 %d/100，review verdict=%s，無需 retry。", selection.DisplaySubTaskIdx, selection.SubTaskReview.Score, verdict)
}

func composeRetryPrompt(description, verdict string, score int, feedback string, issueTags []string, previousResult string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Retry — 上一輪 review 給 %s %d/100]\n\n", strings.TrimSpace(verdict), score))
	b.WriteString("原 sub-task 描述：\n")
	b.WriteString(strings.TrimSpace(description))
	if strings.TrimSpace(previousResult) != "" {
		b.WriteString("\n\n上一輪 sub-task 輸出：\n")
		b.WriteString(truncateForRetryPrompt(previousResult, 6000))
	}
	b.WriteString("\n\nReviewer 找出的問題：\n")
	if strings.TrimSpace(feedback) == "" {
		b.WriteString("（reviewer 未提供詳細文字回饋）")
	} else {
		b.WriteString(strings.TrimSpace(feedback))
	}
	b.WriteString("\n\n請只針對以下問題修正，不要做超出範圍的改動：\n")
	if len(issueTags) == 0 {
		b.WriteString("- 未分類 review 問題\n")
	} else {
		for _, tag := range issueTags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				b.WriteString("- ")
				b.WriteString(tag)
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\n完成後請用可被 reviewer 驗證的格式回報：實際改動或驗證結果、關鍵檔案行號、執行過的命令與 PASS/FAIL、仍未驗證的風險。")
	b.WriteString("\n不要只給兩行摘要；如果沒有做程式碼改動，也要提供足夠證據說明原 review 問題已被補上。")
	return b.String()
}

func (s *SQLiteStorage) selectRetryTargetLatest(ctx context.Context, key chatKey) (retrySelection, error) {
	return s.scanRetrySelection(ctx, `
		SELECT t.id, t.chat_id, t.thread_id, t.project_dir, t.goal, t.engine, t.backend, t.status,
		       t.github_issue_number, t.started_at, t.ended_at,
		       t.total_input_tokens, t.total_output_tokens, t.total_cost_usd,
		       st.id, st.task_id, st.idx, st.description, st.model, st.status, st.result_text,
		       st.input_tokens, st.output_tokens, st.cost_usd, st.started_at, st.ended_at,
		       st.routing_reason, st.routing_latency_ms,
		       rr.id, rr.task_id, rr.reviewer_model, rr.verdict, rr.overall_score, rr.feedback_text,
		       rr.issue_tags, rr.input_tokens, rr.output_tokens, rr.cost_usd, rr.source, rr.created_at,
		       rs.id, rs.review_id, rs.sub_task_id, rs.score, rs.feedback, rs.issue_tags
		FROM review_results rr
		JOIN tasks t ON t.id = rr.task_id
		JOIN review_subtask_results rs ON rs.review_id = rr.id
		JOIN sub_tasks st ON st.id = rs.sub_task_id
		WHERE (rr.verdict IN ('partial', 'fail') OR rs.score < 70)
		  AND t.chat_id = ? AND t.thread_id = ?
		ORDER BY rr.created_at DESC, rr.id DESC, rs.score ASC, st.idx ASC
		LIMIT 1`, key.chatID, key.threadID)
}

func (s *SQLiteStorage) selectRetryTaskCandidates(ctx context.Context, key chatKey, limit int) ([]retryTaskCandidate, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.goal, t.github_issue_number,
		       SUM(CASE WHEN rs.score < 70 THEN 1 ELSE 0 END),
		       MAX(rr.created_at)
		FROM review_results rr
		JOIN tasks t ON t.id = rr.task_id
		JOIN review_subtask_results rs ON rs.review_id = rr.id
		WHERE (rr.verdict IN ('partial', 'fail') OR rs.score < 70)
		  AND t.chat_id = ? AND t.thread_id = ?
		  AND rr.id = (
		    SELECT rr2.id
		    FROM review_results rr2
		    WHERE rr2.task_id = t.id
		    ORDER BY rr2.created_at DESC, rr2.id DESC
		    LIMIT 1
		  )
		GROUP BY t.id, t.goal, t.github_issue_number
		ORDER BY MAX(rr.created_at) DESC, t.id DESC
		LIMIT ?`, key.chatID, key.threadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []retryTaskCandidate
	for rows.Next() {
		var candidate retryTaskCandidate
		var latest sql.NullString
		if err := rows.Scan(&candidate.ID, &candidate.Goal, &candidate.GithubIssueNumber, &candidate.FailedCount, &latest); err != nil {
			return nil, err
		}
		candidate.LatestReviewAt = parseDBTime(latest.String)
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteStorage) selectRetryTargetLowest(ctx context.Context, taskID string) (retrySelection, error) {
	taskID, err := s.resolveRetryTaskID(ctx, taskID)
	if err != nil {
		return retrySelection{}, err
	}
	return s.scanRetrySelection(ctx, `
		SELECT t.id, t.chat_id, t.thread_id, t.project_dir, t.goal, t.engine, t.backend, t.status,
		       t.github_issue_number, t.started_at, t.ended_at,
		       t.total_input_tokens, t.total_output_tokens, t.total_cost_usd,
		       st.id, st.task_id, st.idx, st.description, st.model, st.status, st.result_text,
		       st.input_tokens, st.output_tokens, st.cost_usd, st.started_at, st.ended_at,
		       st.routing_reason, st.routing_latency_ms,
		       rr.id, rr.task_id, rr.reviewer_model, rr.verdict, rr.overall_score, rr.feedback_text,
		       rr.issue_tags, rr.input_tokens, rr.output_tokens, rr.cost_usd, rr.source, rr.created_at,
		       rs.id, rs.review_id, rs.sub_task_id, rs.score, rs.feedback, rs.issue_tags
		FROM review_results rr
		JOIN tasks t ON t.id = rr.task_id
		JOIN review_subtask_results rs ON rs.review_id = rr.id
		JOIN sub_tasks st ON st.id = rs.sub_task_id
		WHERE rr.task_id = ?
		ORDER BY rr.created_at DESC, rr.id DESC, rs.score ASC, st.idx ASC
		LIMIT 1`, taskID)
}

func (s *SQLiteStorage) selectRetryTargetByIndex(ctx context.Context, taskID string, displayIdx int) (retrySelection, error) {
	if displayIdx <= 0 {
		return retrySelection{}, fmt.Errorf("sub-task idx must be >= 1")
	}
	taskID, err := s.resolveRetryTaskID(ctx, taskID)
	if err != nil {
		return retrySelection{}, err
	}
	subTaskIdx := displayIdx - 1
	var subTaskID string
	err = s.db.QueryRowContext(ctx, `
		SELECT id
		FROM sub_tasks
		WHERE task_id = ? AND idx = ?
		LIMIT 1`, taskID, subTaskIdx).Scan(&subTaskID)
	if err == sql.ErrNoRows {
		return retrySelection{}, fmt.Errorf("找不到 task %s 的 sub-task #%d", shortHermesTaskID(taskID), displayIdx)
	}
	if err != nil {
		return retrySelection{}, err
	}

	var latestReviewID int64
	err = s.db.QueryRowContext(ctx, `
		SELECT id
		FROM review_results
		WHERE task_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, taskID).Scan(&latestReviewID)
	if err == sql.ErrNoRows {
		return retrySelection{}, fmt.Errorf("task %s 尚無 review 結果可 retry", shortHermesTaskID(taskID))
	}
	if err != nil {
		return retrySelection{}, err
	}

	var reviewSubTaskID int64
	err = s.db.QueryRowContext(ctx, `
		SELECT id
		FROM review_subtask_results
		WHERE review_id = ? AND sub_task_id = ?
		LIMIT 1`, latestReviewID, subTaskID).Scan(&reviewSubTaskID)
	if err == sql.ErrNoRows {
		return retrySelection{}, fmt.Errorf("最新 review 沒有 sub-task #%d 的評分資料，請改用通知列出的 #<n> 或 /retry %s all-failed", displayIdx, shortHermesTaskID(taskID))
	}
	if err != nil {
		return retrySelection{}, err
	}

	return s.scanRetrySelection(ctx, `
		SELECT t.id, t.chat_id, t.thread_id, t.project_dir, t.goal, t.engine, t.backend, t.status,
		       t.github_issue_number, t.started_at, t.ended_at,
		       t.total_input_tokens, t.total_output_tokens, t.total_cost_usd,
		       st.id, st.task_id, st.idx, st.description, st.model, st.status, st.result_text,
		       st.input_tokens, st.output_tokens, st.cost_usd, st.started_at, st.ended_at,
		       st.routing_reason, st.routing_latency_ms,
		       rr.id, rr.task_id, rr.reviewer_model, rr.verdict, rr.overall_score, rr.feedback_text,
		       rr.issue_tags, rr.input_tokens, rr.output_tokens, rr.cost_usd, rr.source, rr.created_at,
		       rs.id, rs.review_id, rs.sub_task_id, rs.score, rs.feedback, rs.issue_tags
		FROM review_results rr
		JOIN tasks t ON t.id = rr.task_id
		JOIN sub_tasks st ON st.task_id = t.id
		JOIN review_subtask_results rs ON rs.review_id = rr.id AND rs.sub_task_id = st.id
		WHERE rr.id = ? AND st.id = ?
		ORDER BY rr.created_at DESC, rr.id DESC
		LIMIT 1`, latestReviewID, subTaskID)
}

func (s *SQLiteStorage) selectRetryTargetsAllFailed(ctx context.Context, taskID string) ([]retrySelection, error) {
	taskID, err := s.resolveRetryTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.chat_id, t.thread_id, t.project_dir, t.goal, t.engine, t.backend, t.status,
		       t.github_issue_number, t.started_at, t.ended_at,
		       t.total_input_tokens, t.total_output_tokens, t.total_cost_usd,
		       st.id, st.task_id, st.idx, st.description, st.model, st.status, st.result_text,
		       st.input_tokens, st.output_tokens, st.cost_usd, st.started_at, st.ended_at,
		       st.routing_reason, st.routing_latency_ms,
		       rr.id, rr.task_id, rr.reviewer_model, rr.verdict, rr.overall_score, rr.feedback_text,
		       rr.issue_tags, rr.input_tokens, rr.output_tokens, rr.cost_usd, rr.source, rr.created_at,
		       rs.id, rs.review_id, rs.sub_task_id, rs.score, rs.feedback, rs.issue_tags
		FROM review_results rr
		JOIN tasks t ON t.id = rr.task_id
		JOIN review_subtask_results rs ON rs.review_id = rr.id
		JOIN sub_tasks st ON st.id = rs.sub_task_id
		WHERE rr.id = (SELECT id FROM review_results WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT 1)
		  AND rs.score < 70
		ORDER BY rs.score ASC, st.idx ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []retrySelection
	for rows.Next() {
		selection, err := scanRetrySelectionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, selection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.populateRetryCount(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *SQLiteStorage) resolveRetryTaskID(ctx context.Context, taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf("missing task_id")
	}
	if issueNumber, ok := parseRetryIssueRef(taskID); ok {
		return s.resolveRetryTaskIDByIssue(ctx, issueNumber)
	}
	var exact string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id = ? LIMIT 1`, taskID).Scan(&exact)
	if err == nil {
		return exact, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tasks WHERE id LIKE ? ORDER BY started_at DESC, id DESC LIMIT 2`, taskID+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matches = append(matches, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("找不到 task_id %q", taskID)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("task_id prefix %q 不唯一，請輸入更長的 task_id", taskID)
	}
}

func parseRetryIssueRef(ref string) (int, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "#") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(ref, "#"))
	if err != nil || n <= 0 {
		return 0, true
	}
	return n, true
}

func (s *SQLiteStorage) resolveRetryTaskIDByIssue(ctx context.Context, issueNumber int) (string, error) {
	if issueNumber <= 0 {
		return "", fmt.Errorf("無效的 issue 編號")
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM tasks
		WHERE github_issue_number = ?
		  AND EXISTS (SELECT 1 FROM review_results rr WHERE rr.task_id = tasks.id)
		ORDER BY CASE WHEN project_dir != '' THEN 0 ELSE 1 END, started_at DESC, id DESC
		LIMIT 1`, issueNumber).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT id
		FROM tasks
		WHERE goal LIKE ?
		  AND EXISTS (SELECT 1 FROM review_results rr WHERE rr.task_id = tasks.id)
		ORDER BY CASE WHEN project_dir != '' THEN 0 ELSE 1 END, started_at DESC, id DESC
		LIMIT 1`, fmt.Sprintf("[GitHub #%d]%%", issueNumber)).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("找不到 GitHub Issue #%d 對應且已有 review 結果的 Hermes task", issueNumber)
	}
	return "", err
}

func (s *SQLiteStorage) scanRetrySelection(ctx context.Context, query string, args ...any) (retrySelection, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	selection, err := scanRetrySelectionRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return retrySelection{}, fmt.Errorf("找不到可 retry 的 review sub-task")
		}
		return retrySelection{}, err
	}
	if err := s.populateRetryCount(ctx, &selection); err != nil {
		return retrySelection{}, err
	}
	return selection, nil
}

func scanRetrySelectionRow(scanner sqlScanner) (retrySelection, error) {
	var selection retrySelection
	var taskStarted, taskEnded, subStarted, subEnded, reviewCreated sql.NullString
	var reviewTagsJSON, subReviewTagsJSON string
	err := scanner.Scan(
		&selection.Task.ID, &selection.Task.ChatID, &selection.Task.ThreadID, &selection.Task.ProjectDir,
		&selection.Task.Goal, &selection.Task.Engine, &selection.Task.Backend, &selection.Task.Status,
		&selection.Task.GithubIssueNumber, &taskStarted, &taskEnded, &selection.Task.TotalInputTokens, &selection.Task.TotalOutputTokens,
		&selection.Task.TotalCostUSD,
		&selection.SubTask.ID, &selection.SubTask.TaskID, &selection.SubTask.Idx, &selection.SubTask.Description,
		&selection.SubTask.Model, &selection.SubTask.Status, &selection.SubTask.ResultText,
		&selection.SubTask.InputTokens, &selection.SubTask.OutputTokens, &selection.SubTask.CostUSD,
		&subStarted, &subEnded, &selection.SubTask.RoutingReason, &selection.SubTask.RoutingLatencyMS,
		&selection.Review.ID, &selection.Review.TaskID, &selection.Review.ReviewerModel, &selection.Review.Verdict,
		&selection.Review.OverallScore, &selection.Review.FeedbackText, &reviewTagsJSON,
		&selection.Review.InputTokens, &selection.Review.OutputTokens, &selection.Review.CostUSD,
		&selection.Review.Source, &reviewCreated,
		&selection.SubTaskReview.ID, &selection.SubTaskReview.ReviewID, &selection.SubTaskReview.SubTaskID,
		&selection.SubTaskReview.Score, &selection.SubTaskReview.Feedback, &subReviewTagsJSON,
	)
	if err != nil {
		return selection, err
	}
	selection.Task.StartedAt = parseDBTime(taskStarted.String)
	selection.Task.EndedAt = parseNullableDBTime(taskEnded)
	selection.SubTask.StartedAt = parseDBTime(subStarted.String)
	selection.SubTask.EndedAt = parseNullableDBTime(subEnded)
	selection.Review.CreatedAt = parseDBTime(reviewCreated.String)
	selection.Review.IssueTags = parseStringListJSON(reviewTagsJSON)
	selection.SubTaskReview.IssueTags = parseStringListJSON(subReviewTagsJSON)
	selection.DisplaySubTaskIdx = selection.SubTask.Idx + 1
	return selection, nil
}

func (s *SQLiteStorage) populateRetryCount(ctx context.Context, selection *retrySelection) error {
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM review_results rr
		JOIN review_subtask_results rs ON rs.review_id = rr.id
		WHERE rr.source = 'retry' AND rs.sub_task_id = ?`, selection.SubTask.ID).Scan(&selection.RetryCount); err != nil {
		return err
	}
	var codexReviews int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM review_results
		WHERE task_id = ?
		  AND (lower(reviewer_model) LIKE 'gpt%' OR lower(reviewer_model) LIKE 'codex%')`,
		selection.Task.ID,
	).Scan(&codexReviews); err != nil {
		return err
	}
	selection.PreferCodex = codexReviews > 0
	return nil
}

func parseRetryArgs(parts []string) (mode, taskID string, idx int, err error) {
	if len(parts) < 2 || strings.EqualFold(parts[1], "latest") {
		return "latest", "", 0, nil
	}
	if strings.EqualFold(parts[1], "all-failed") {
		return "all-failed-smart", "", 0, nil
	}
	taskID = strings.TrimSpace(parts[1])
	if taskID == "" {
		return "", "", 0, fmt.Errorf("missing task_id")
	}
	if len(parts) == 2 {
		return "lowest", taskID, 0, nil
	}
	if strings.EqualFold(parts[2], "all-failed") {
		return "all-failed", taskID, 0, nil
	}
	idx, err = strconv.Atoi(parts[2])
	if err != nil || idx <= 0 {
		return "", "", 0, fmt.Errorf("idx must be a positive integer")
	}
	return "index", taskID, idx, nil
}

func (t *TelegramBot) handleRetryCommand(key chatKey, parts []string) {
	if globalStorage == nil {
		t.send(key, "❌ Storage 尚未啟用，無法讀取 review 結果。")
		return
	}
	store, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		t.send(key, "❌ 目前 storage backend 不支援 /retry。")
		return
	}

	mode, taskID, idx, err := parseRetryArgs(parts)
	if err != nil {
		t.send(key, "❌ 使用方式：/retry latest、/retry <task_id|#issue>、/retry <task_id|#issue> <idx>、/retry <task_id|#issue> all-failed")
		return
	}
	if mode == "all-failed-smart" {
		t.sendRetryAllFailedResolution(key)
		return
	}

	go func() {
		trackDone := globalJobTracker.Start("retry.command")
		var jobErr error
		defer func() { trackDone(jobErr) }()
		commandID := fmt.Sprintf("retry-%d", time.Now().UnixNano())
		started := time.Now()
		log.Printf("[retry] %s start chat=%d thread=%d mode=%s task=%q idx=%d", commandID, key.chatID, key.threadID, mode, taskID, idx)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[retry] %s panic: %v", commandID, r)
				t.send(key, fmt.Sprintf("❌ Retry 發生未預期錯誤（%s）：%v", commandID, r))
			}
			log.Printf("[retry] %s done duration=%s", commandID, time.Since(started).Round(time.Millisecond))
		}()

		selectCtx, cancelSelect := context.WithTimeout(context.Background(), retrySelectionTimeout)
		defer cancelSelect()
		var selections []retrySelection
		switch mode {
		case "latest":
			selection, selectErr := store.selectRetryTargetLatest(selectCtx, key)
			err = selectErr
			if err == nil {
				selections = []retrySelection{selection}
			}
		case "lowest":
			selection, selectErr := store.selectRetryTargetLowest(selectCtx, taskID)
			err = selectErr
			if err == nil {
				selections = []retrySelection{selection}
			}
		case "index":
			selection, selectErr := store.selectRetryTargetByIndex(selectCtx, taskID, idx)
			err = selectErr
			if err == nil {
				if !retrySelectionNeedsRetry(selection) {
					t.send(key, formatRetryNoRetryNeeded(selection))
					return
				}
				selections = []retrySelection{selection}
			}
		case "all-failed":
			selections, err = store.selectRetryTargetsAllFailed(selectCtx, taskID)
		default:
			err = fmt.Errorf("unknown retry mode %q", mode)
		}
		if err != nil {
			jobErr = err
			t.send(key, "❌ "+err.Error())
			return
		}
		if len(selections) == 0 {
			t.send(key, "✅ 找不到低分或 partial/fail 的 sub-task 可 retry。")
			return
		}

		for i, selection := range selections {
			if decision := decideSubTaskRetryRecovery(selection); decision.Action != appengine.RecoveryActionRetry {
				t.send(key, fmt.Sprintf("⛔ sub-task #%d 已 retry %d 次，達到上限，建議人工介入。", selection.DisplaySubTaskIdx, selection.RetryCount))
				continue
			}
			if len(selections) > 1 {
				t.send(key, fmt.Sprintf("🔁 all-failed retry %d/%d", i+1, len(selections)))
			}
			if mode == "index" && selection.SubTaskReview.Score >= 70 {
				t.send(key, fmt.Sprintf("ℹ️ sub-task #%d 目前評分 %d/100，仍依人工指定執行 retry。", selection.DisplaySubTaskIdx, selection.SubTaskReview.Score))
			}
			runCtx, cancelRun := context.WithTimeout(context.Background(), t.retryAttemptTimeout())
			t.registerRetryInflight(commandID, key, selection.DisplaySubTaskIdx, cancelRun)
			outcome, runErr := t.runSubTaskRetry(runCtx, key, store, selection)
			cancelRun()
			t.unregisterRetryInflight(commandID)
			if runErr != nil {
				jobErr = runErr
				t.send(key, formatRetryFailure(outcome, runErr))
				continue
			}
			t.send(key, formatRetryCompletion(outcome))
		}
	}()
}

func (t *TelegramBot) retryAttemptTimeout() time.Duration {
	minutes := 15
	if t != nil && t.config != nil && t.config.CLITimeoutMinutes > 0 {
		minutes = t.config.CLITimeoutMinutes
	}
	return time.Duration(minutes*2+5) * time.Minute
}

func (t *TelegramBot) runSubTaskRetry(ctx context.Context, key chatKey, store *SQLiteStorage, selection retrySelection) (retryOutcome, error) {
	start := time.Now()
	description := truncateForTelegram(selection.SubTask.Description, 80)
	t.send(key, fmt.Sprintf(
		"🔄 Retrying sub-task #%d：「%s」\n（原分數 %d/100，%d 個 issue tag）\n\n⏳ 執行中...",
		selection.DisplaySubTaskIdx,
		description,
		selection.SubTaskReview.Score,
		len(selection.SubTaskReview.IssueTags),
	))

	prompt := composeRetryPrompt(
		selection.SubTask.Description,
		selection.Review.Verdict,
		selection.SubTaskReview.Score,
		selection.SubTaskReview.Feedback,
		selection.SubTaskReview.IssueTags,
		selection.SubTask.ResultText,
	)
	agent := t.getAgent(key)
	projectDir := t.retryProjectDir(selection, agent)
	if projectDir != "" {
		agent.SetProject(projectDir)
	}
	if agent.IsProcessing() {
		return retryOutcome{Selection: selection, Duration: time.Since(start)}, fmt.Errorf("目前這個 topic 的 bot 還在執行上一個請求，請等它完成或先按 Stop 後再 retry")
	}

	progress := newTelegramProgressSink(func(update string, silent bool) {
		if !silent && strings.TrimSpace(update) != "" {
			t.send(key, update)
		}
	})
	retryModel := t.retryExecutionModel(key, selection)
	prevOverride := agent.currentModelOverride
	if retryModel != "" {
		agent.currentModelOverride = retryModel
		agent.ClearSessionForModel(retryModel)
	}
	// Skip the general-memory + recent-message bridge for the duration of
	// this retry. composeRetryPrompt already carries the prior failed result
	// and the reviewer's feedback; bridge cards re-inject prior runs and add
	// 6 KB+ of redundant context that pushes codex toward "Prompt is too
	// long" and stretches each retry to 5-15 minutes.
	agent.SetSuppressMemoryBridge(true)
	defer agent.SetSuppressMemoryBridge(false)

	stopHeartbeat := t.startRetryHeartbeat(ctx, key, start, selection.DisplaySubTaskIdx)
	defer stopHeartbeat()
	result, err := t.runRetryDirectWithWatchdog(ctx, agent, prompt, progress)
	agent.currentModelOverride = prevOverride
	if err != nil {
		outcome := retryOutcome{Selection: selection, Result: result, Duration: time.Since(start)}
		if persistErr := storeRetryFailureResult(store, selection, result, err); persistErr != nil {
			log.Printf("[retry] persist retry failure task=%s subtask=%s: %v", selection.Task.ID, selection.SubTask.ID, persistErr)
		}
		return outcome, err
	}

	reviewModel := t.retryReviewModel(key)
	reviewer := NewCLIReviewPhase(t.client, reviewModel)
	review, err := reviewer.Review(ctx, appengine.ReviewRequest{
		TaskID:      selection.Task.ID,
		ProjectDir:  projectDir,
		Goal:        selection.Task.Goal,
		Accumulated: buildRetryReviewAccumulated(selection, result.Text),
		Artifacts:   result.Artifacts,
		SubTaskResults: []appengine.ReviewSubTaskInput{{
			ID:          selection.SubTask.ID,
			Index:       selection.SubTask.Idx,
			Description: selection.SubTask.Description,
			Status:      "done",
			Result:      buildRetryReviewSubTaskResult(selection, result.Text),
		}},
	})
	if err != nil {
		return retryOutcome{Selection: selection, Result: result, Duration: time.Since(start)}, fmt.Errorf("review retry result: %w", err)
	}
	review = normalizeRetryReviewForSubTask(review, selection.SubTask.ID)
	if err := store.StoreReviewWithSource(ctx, selection.Task.ID, review, "retry"); err != nil {
		return retryOutcome{Selection: selection, Result: result, Review: review, Duration: time.Since(start)}, fmt.Errorf("store retry review: %w", err)
	}
	BroadcastReviewEventWithSource(appengine.BuildReviewNotification(selection.Task.ID, review), "retry")

	// Write the retry's improved result back into the hermes task plan so
	// future continuations and BuildExecutorPrompt's "前序子任務結果" block
	// see the latest version. Without this the canonical Plan[i].Result still
	// holds the original failed output and M1's prior-subtask injection
	// replays the failure into every subsequent sub-task. Gated on
	// plan-execute tasks (where the hermes table is the source of truth) and
	// on a score improvement to avoid overwriting with a worse retry.
	newScore := retryScoreForSubTask(review, selection.SubTask.ID)
	if selection.Task.Engine == "plan-execute" && t.taskSvc != nil && selection.SubTask.Idx >= 0 && newScore >= selection.SubTaskReview.Score {
		if upErr := t.taskSvc.UpdateSubTask(selection.Task.ID, selection.SubTask.Idx, hermes.SubTaskDone, result.Text, 0); upErr != nil {
			log.Printf("[retry] write-back to hermes plan failed task=%s idx=%d: %v", selection.Task.ID, selection.SubTask.Idx, upErr)
		} else {
			log.Printf("[retry] wrote retry result back to hermes plan task=%s idx=%d (score %d → %d)", selection.Task.ID, selection.SubTask.Idx, selection.SubTaskReview.Score, newScore)
		}
	}

	return retryOutcome{
		Selection: selection,
		Result:    result,
		Review:    review,
		Improved:  retryScoreForSubTask(review, selection.SubTask.ID) > selection.SubTaskReview.Score,
		Duration:  time.Since(start),
	}, nil
}

func (t *TelegramBot) runRetryDirectWithWatchdog(ctx context.Context, agent *Agent, prompt string, progress appengine.ProgressSink) (appengine.Result, error) {
	type retryRunResult struct {
		result appengine.Result
		err    error
	}
	done := make(chan retryRunResult, 1)
	go func() {
		result, err := appengine.NewDirectEngine(agent).Run(ctx, prompt, agent.chatContext, progress)
		done <- retryRunResult{result: result, err: err}
	}()

	select {
	case outcome := <-done:
		return outcome.result, outcome.err
	case <-ctx.Done():
		if agent != nil {
			agent.Abort()
		}
		select {
		case outcome := <-done:
			if outcome.err != nil {
				return outcome.result, outcome.err
			}
			return outcome.result, ctx.Err()
		case <-time.After(5 * time.Second):
			return appengine.Result{}, fmt.Errorf("retry execution timed out or was canceled: %w", ctx.Err())
		}
	}
}

// retryHeartbeatInterval controls how often "still running" pings go out
// during a long retry. Operators kept retrying because the initial
// "🔄 Retrying..." line was the only signal and codex calls routinely take
// 5-15 minutes; the heartbeat costs one Telegram message per minute and
// removes the "is it stuck?" guesswork.
const retryHeartbeatInterval = 60 * time.Second

// startRetryHeartbeat fires a periodic "still running" message until ctx is
// cancelled. Returns a stop function the caller defers.
func (t *TelegramBot) startRetryHeartbeat(ctx context.Context, key chatKey, started time.Time, displayIdx int) func() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(retryHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(started).Round(time.Second)
				t.sendSilent(key, fmt.Sprintf("⏳ Sub-task #%d retry 仍在執行（已耗時 %s）", displayIdx, elapsed))
			}
		}
	}()
	return func() { close(stop) }
}

func storeRetryFailureResult(store *SQLiteStorage, selection retrySelection, result appengine.Result, runErr error) error {
	if store == nil {
		return nil
	}
	now := time.Now()
	subTask := selection.SubTask
	subTask.Status = "failed"
	subTask.ResultText = buildRetryFailureResult(result, runErr)
	subTask.EndedAt = &now
	if subTask.StartedAt.IsZero() {
		subTask.StartedAt = now
	}
	return store.UpsertUnifiedSubTask(subTask)
}

func (t *TelegramBot) retryProjectDir(selection retrySelection, agent *Agent) string {
	if projectDir := strings.TrimSpace(selection.Task.ProjectDir); projectDir != "" {
		return projectDir
	}
	if agent != nil {
		if projectDir := strings.TrimSpace(agent.ProjectDir()); projectDir != "" {
			return projectDir
		}
	}
	if t != nil && t.config != nil {
		if projectDir := strings.TrimSpace(t.config.DefaultProjectDir); projectDir != "" {
			return projectDir
		}
	}
	return "."
}

func (t *TelegramBot) retryExecutionModel(key chatKey, selection retrySelection) string {
	if t == nil || t.config == nil {
		return ""
	}
	if prefModel := t.retryModelForUserPreference(key); prefModel != "" {
		return prefModel
	}
	cfg := HermesDefaults(t.config.Hermes)
	backendHint := strings.Join([]string{
		selection.SubTask.Model,
		selection.Review.ReviewerModel,
		selection.Task.Backend,
	}, " ")
	if selection.PreferCodex || strings.Contains(strings.ToLower(backendHint), "gpt") || strings.Contains(strings.ToLower(backendHint), "codex") {
		if modelRequiresCodex(cfg.CodexHeavyExecutorModel) {
			return cfg.CodexHeavyExecutorModel
		}
		if t.config.ModelRouting.CodexSmartModel != "" {
			return t.config.ModelRouting.CodexSmartModel
		}
		if modelRequiresCodex(cfg.CodexExecutorModel) {
			return cfg.CodexExecutorModel
		}
		return t.config.ModelRouting.CodexDeepModel
	}
	if cfg.HeavyExecutorModel != "" {
		return cfg.HeavyExecutorModel
	}
	if t.config.ModelRouting.SmartModel != "" {
		return t.config.ModelRouting.SmartModel
	}
	if cfg.ExecutorModel != "" {
		return cfg.ExecutorModel
	}
	return t.config.ModelRouting.DeepModel
}

func (t *TelegramBot) retryModelForUserPreference(key chatKey) string {
	return t.modelForUserPreference(key)
}

func (t *TelegramBot) retryReviewModel(key chatKey) string {
	if t == nil || t.config == nil {
		return ""
	}
	pref := t.getUserModelPreference(key)
	if strings.HasPrefix(pref, "gpt-") {
		if t.config.ModelRouting.CodexDeepModel != "" {
			return t.config.ModelRouting.CodexDeepModel
		}
		if t.config.ModelRouting.CodexSmartModel != "" {
			return t.config.ModelRouting.CodexSmartModel
		}
	}
	return t.config.ModelRouting.DeepModel
}

func retryScoreForSubTask(review appengine.ReviewResult, subTaskID string) int {
	for _, subTask := range review.SubTaskResults {
		if subTask.SubTaskID == subTaskID {
			return subTask.Score
		}
	}
	return review.OverallScore
}

func buildRetryReviewAccumulated(selection retrySelection, retryResult string) string {
	var b strings.Builder
	b.WriteString("Retry review context.\n\nOriginal reviewer feedback:\n")
	b.WriteString(strings.TrimSpace(selection.SubTaskReview.Feedback))
	if len(selection.SubTaskReview.IssueTags) > 0 {
		b.WriteString("\nIssue tags: ")
		b.WriteString(strings.Join(selection.SubTaskReview.IssueTags, ", "))
	}
	b.WriteString("\n\nPrevious sub-task result:\n")
	b.WriteString(truncateForRetryPrompt(selection.SubTask.ResultText, 6000))
	b.WriteString("\n\nRetry result:\n")
	b.WriteString(strings.TrimSpace(retryResult))
	return b.String()
}

func buildRetryReviewSubTaskResult(selection retrySelection, retryResult string) string {
	var b strings.Builder
	b.WriteString("Previous result:\n")
	b.WriteString(truncateForRetryPrompt(selection.SubTask.ResultText, 6000))
	b.WriteString("\n\nRetry result:\n")
	b.WriteString(strings.TrimSpace(retryResult))
	return b.String()
}

func buildRetryFailureResult(result appengine.Result, runErr error) string {
	var b strings.Builder
	b.WriteString("Retry failed.\n\nError:\n")
	if runErr != nil {
		b.WriteString(strings.TrimSpace(runErr.Error()))
	}
	if partial := strings.TrimSpace(result.Text); partial != "" {
		b.WriteString("\n\nPartial output captured before failure:\n")
		b.WriteString(partial)
	}
	return strings.TrimSpace(b.String())
}

func normalizeRetryReviewForSubTask(review appengine.ReviewResult, subTaskID string) appengine.ReviewResult {
	if len(review.SubTaskResults) == 0 {
		review.SubTaskResults = []appengine.ReviewSubTaskResult{{
			SubTaskID: subTaskID,
			Score:     review.OverallScore,
			Feedback:  review.Feedback,
			IssueTags: append([]appengine.ReviewTag(nil), review.IssueTags...),
		}}
		return review
	}
	// Always override SubTaskID — the reviewer may return an arbitrary string
	// that doesn't match the actual sub_tasks.id in the DB, causing FK failures.
	// In a single-subtask retry we always know the exact target ID.
	for i := range review.SubTaskResults {
		review.SubTaskResults[i].SubTaskID = subTaskID
	}
	return review
}

func formatRetryCompletion(outcome retryOutcome) string {
	newScore := retryScoreForSubTask(outcome.Review, outcome.Selection.SubTask.ID)
	delta := newScore - outcome.Selection.SubTaskReview.Score
	reviewPassed := retryReviewPassed(outcome.Review.Verdict)
	status := retryCompletionStatus(reviewPassed, delta)
	message := fmt.Sprintf(
		"%s\n   原分數: %d → %d (%+d)\n   驗證: %s\n   耗時: %s",
		status,
		outcome.Selection.SubTaskReview.Score,
		newScore,
		delta,
		outcome.Review.Verdict,
		outcome.Duration.Round(time.Second),
	)
	if result := truncateRetryMessageField(outcome.Result.Text, 700); result != "" {
		message += "\n\n執行結果：\n" + result
	}
	if feedback := truncateRetryMessageField(retryReviewFeedbackForSubTask(outcome.Review, outcome.Selection.SubTask.ID), 500); feedback != "" {
		message += "\n\nReview 回饋：\n" + feedback
	}
	if tags := retryReviewTagsForSubTask(outcome.Review, outcome.Selection.SubTask.ID); tags != "" {
		message += "\nIssue tags: " + tags
	}
	if !reviewPassed {
		message += "\n\n判讀：Retry job 已結束，但 reviewer 仍判定未通過；這不代表 issue 已完成。"
		message += "\n下一步：依 Review 回饋和 issue tags 修正實作或補齊成功路徑驗證，再重新 retry。"
	}
	return message
}

func retryReviewPassed(verdict appengine.ReviewVerdict) bool {
	switch verdict {
	case appengine.VerdictPass, appengine.VerdictAllow:
		return true
	default:
		return false
	}
}

func retryCompletionStatus(reviewPassed bool, delta int) string {
	if reviewPassed {
		if delta <= 0 {
			return "✅ Retry 驗證通過（分數未提升）"
		}
		return "✅ Retry 驗證通過"
	}
	if delta > 0 {
		return "⚠️ Retry 執行完成，但驗證仍未通過"
	}
	return "❌ Retry 執行完成，驗證仍未通過且未改善"
}

func formatRetryFailure(outcome retryOutcome, runErr error) string {
	message := "❌ Retry 失敗：" + strings.TrimSpace(runErr.Error())
	if partial := truncateRetryMessageField(outcome.Result.Text, 900); partial != "" {
		message += "\n\n已捕捉到的 partial output：\n" + partial
	}
	if outcome.Duration > 0 {
		message += "\n\n耗時: " + outcome.Duration.Round(time.Second).String()
	}
	return message
}

func retryReviewFeedbackForSubTask(review appengine.ReviewResult, subTaskID string) string {
	for _, subTask := range review.SubTaskResults {
		if subTask.SubTaskID == subTaskID && strings.TrimSpace(subTask.Feedback) != "" {
			return subTask.Feedback
		}
	}
	return review.Feedback
}

func retryReviewTagsForSubTask(review appengine.ReviewResult, subTaskID string) string {
	tags := review.IssueTags
	for _, subTask := range review.SubTaskResults {
		if subTask.SubTaskID == subTaskID {
			tags = subTask.IssueTags
			break
		}
	}
	if len(tags) == 0 {
		return ""
	}
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != "" {
			items = append(items, string(tag))
		}
	}
	return strings.Join(items, ", ")
}

func truncateRetryMessageField(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func truncateForTelegram(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
}

func truncateForRetryPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
