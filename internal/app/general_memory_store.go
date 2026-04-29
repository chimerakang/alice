package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *SQLiteStorage) ListGeneralMemoryCards(ctx context.Context, req MemoryRequest, limit int) ([]GeneralMemoryCard, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 3
	}

	issueNumber := normalizedMemoryIssueNumber(req)
	clauses := []string{
		"t.chat_id = ?",
		"t.status = 'done'",
		"t.engine <> 'hermes'",
	}
	args := []any{req.ChatID}
	if strings.TrimSpace(req.ProjectDir) != "" {
		clauses = append(clauses, "t.project_dir = ?")
		args = append(args, req.ProjectDir)
	}
	if issueNumber > 0 {
		clauses = append(clauses, "(t.github_issue_number = ? OR t.goal LIKE ? OR t.goal LIKE ?)")
		args = append(args, issueNumber, fmt.Sprintf("%%#%d%%", issueNumber), fmt.Sprintf("%%＃%d%%", issueNumber))
	}

	args = append(args, req.ThreadID, limit*4)
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.chat_id, t.thread_id, t.project_dir, t.github_issue_number,
		       t.goal, COALESCE(st.result_text, ''), t.engine, t.backend,
		       COALESCE(st.model, ''), COALESCE(t.ended_at, t.started_at)
		FROM tasks t
		LEFT JOIN sub_tasks st
			ON st.task_id = t.id
			AND st.idx = (
				SELECT MIN(st2.idx)
				FROM sub_tasks st2
				WHERE st2.task_id = t.id
			)
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY
			CASE WHEN t.thread_id = ? THEN 0 ELSE 1 END,
			COALESCE(t.ended_at, t.started_at) DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	currentNorm := normalizeHermesGoal(stripLanguageDirective(req.UserMessage))
	cards := make([]GeneralMemoryCard, 0, limit)
	for rows.Next() {
		card, err := scanGeneralMemoryCard(rows)
		if err != nil {
			return nil, err
		}
		if issueNumber > 0 && card.GithubIssueNumber != issueNumber && !memoryTextReferencesIssue(card.Goal, issueNumber) {
			continue
		}
		if currentNorm != "" && normalizeHermesGoal(stripLanguageDirective(card.Goal)) == currentNorm {
			continue
		}
		cards = append(cards, card)
		if len(cards) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cards, nil
}

func scanGeneralMemoryCard(rows *sql.Rows) (GeneralMemoryCard, error) {
	var card GeneralMemoryCard
	var updatedAt sql.NullString
	err := rows.Scan(
		&card.ID,
		&card.ChatID,
		&card.ThreadID,
		&card.ProjectDir,
		&card.GithubIssueNumber,
		&card.Goal,
		&card.Result,
		&card.Engine,
		&card.Backend,
		&card.Model,
		&updatedAt,
	)
	if err != nil {
		return card, err
	}
	card.UpdatedAt = parseDBTime(updatedAt.String)
	return card, nil
}

func memoryTextReferencesIssue(text string, issueNumber int) bool {
	if issueNumber <= 0 {
		return false
	}
	found, ok := ParseIssueNumber(text)
	return ok && found == issueNumber
}
