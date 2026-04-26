package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type plannerRuleTagCounter struct {
	Count        int
	TaskCount    int
	SubTaskCount int
}

// PlannerRulesIssueTagStat captures a single review issue tag frequency.
type PlannerRulesIssueTagStat struct {
	Tag          string `json:"tag"`
	Count        int    `json:"count"`
	TaskCount    int    `json:"task_count"`
	SubTaskCount int    `json:"sub_task_count"`
}

// PlannerRulesWeeklyReport summarizes review_results over a recent time window.
type PlannerRulesWeeklyReport struct {
	WindowStart          time.Time                  `json:"window_start"`
	WindowEnd            time.Time                  `json:"window_end"`
	GeneratedAt          time.Time                  `json:"generated_at"`
	ReviewCount          int                        `json:"review_count"`
	ReviewedSubTaskCount int                        `json:"reviewed_sub_task_count"`
	AverageOverallScore  float64                    `json:"average_overall_score"`
	VerdictCounts        map[string]int             `json:"verdict_counts"`
	TopIssueTags         []PlannerRulesIssueTagStat `json:"top_issue_tags"`
	Recommendations      []string                   `json:"recommendations"`
}

// GetPlannerRulesWeeklyReport aggregates recent review_results into a human-reviewable
// planner tuning report for /hermes-stats week.
func (s *SQLiteStorage) GetPlannerRulesWeeklyReport(windowStart, windowEnd time.Time) (PlannerRulesWeeklyReport, error) {
	if windowEnd.IsZero() {
		windowEnd = time.Now().UTC()
	}
	if windowStart.IsZero() {
		windowStart = windowEnd.Add(-7 * 24 * time.Hour)
	}
	report := PlannerRulesWeeklyReport{
		WindowStart:   windowStart.UTC(),
		WindowEnd:     windowEnd.UTC(),
		GeneratedAt:   time.Now().UTC(),
		VerdictCounts: map[string]int{"pass": 0, "partial": 0, "fail": 0},
	}

	rows, err := s.db.Query(`
		SELECT id, verdict, overall_score, issue_tags
		FROM review_results
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC`,
		report.WindowStart.Format(time.RFC3339Nano),
		report.WindowEnd.Format(time.RFC3339Nano),
	)
	if err != nil {
		return report, fmt.Errorf("query review_results: %w", err)
	}
	defer rows.Close()

	tagCounts := make(map[string]*plannerRuleTagCounter)
	totalScore := 0

	for rows.Next() {
		var reviewID int64
		var verdict string
		var overallScore int
		var rawTags string
		if err := rows.Scan(&reviewID, &verdict, &overallScore, &rawTags); err != nil {
			return report, fmt.Errorf("scan review_results: %w", err)
		}

		report.ReviewCount++
		totalScore += overallScore
		verdict = strings.TrimSpace(strings.ToLower(verdict))
		if verdict == "" {
			verdict = "unknown"
		}
		report.VerdictCounts[verdict]++

		for _, tag := range decodeIssueTags(rawTags) {
			counter := ensureTagCounter(tagCounts, tag)
			counter.Count++
			counter.TaskCount++
		}
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("iterate review_results: %w", err)
	}

	if report.ReviewCount > 0 {
		report.AverageOverallScore = float64(totalScore) / float64(report.ReviewCount)
	}

	subRows, err := s.db.Query(`
		SELECT rs.issue_tags
		FROM review_subtask_results rs
		JOIN review_results rr ON rr.id = rs.review_id
		WHERE rr.created_at >= ? AND rr.created_at <= ?`,
		report.WindowStart.Format(time.RFC3339Nano),
		report.WindowEnd.Format(time.RFC3339Nano),
	)
	if err != nil {
		return report, fmt.Errorf("query review_subtask_results: %w", err)
	}
	defer subRows.Close()

	for subRows.Next() {
		var rawTags string
		if err := subRows.Scan(&rawTags); err != nil {
			return report, fmt.Errorf("scan review_subtask_results: %w", err)
		}
		report.ReviewedSubTaskCount++
		for _, tag := range decodeIssueTags(rawTags) {
			counter := ensureTagCounter(tagCounts, tag)
			counter.Count++
			counter.SubTaskCount++
		}
	}
	if err := subRows.Err(); err != nil {
		return report, fmt.Errorf("iterate review_subtask_results: %w", err)
	}

	report.TopIssueTags = topPlannerRuleIssueTags(tagCounts, 3)
	report.Recommendations = plannerRuleRecommendations(report.TopIssueTags, report.ReviewCount)
	return report, nil
}

func decodeIssueTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func ensureTagCounter(tagCounts map[string]*plannerRuleTagCounter, tag string) *plannerRuleTagCounter {
	if counter, ok := tagCounts[tag]; ok {
		return counter
	}
	counter := &plannerRuleTagCounter{}
	tagCounts[tag] = counter
	return counter
}

func topPlannerRuleIssueTags(tagCounts map[string]*plannerRuleTagCounter, limit int) []PlannerRulesIssueTagStat {
	stats := make([]PlannerRulesIssueTagStat, 0, len(tagCounts))
	for tag, counter := range tagCounts {
		stats = append(stats, PlannerRulesIssueTagStat{
			Tag:          tag,
			Count:        counter.Count,
			TaskCount:    counter.TaskCount,
			SubTaskCount: counter.SubTaskCount,
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Tag < stats[j].Tag
	})
	if limit > 0 && len(stats) > limit {
		stats = stats[:limit]
	}
	return stats
}

func plannerRuleRecommendations(tags []PlannerRulesIssueTagStat, reviewCount int) []string {
	if reviewCount == 0 {
		return []string{"近 7 天沒有 review_results，先累積樣本後再調整 Planner 規則。"}
	}

	recommendations := make([]string, 0, len(tags)+1)
	if reviewCount < 10 {
		recommendations = append(recommendations, fmt.Sprintf("樣本數僅 %d 筆，建議先觀察到至少 10 筆任務再固化 Planner 規則。", reviewCount))
	}
	for _, tag := range tags {
		switch tag.Tag {
		case "ambiguous_goal":
			recommendations = append(recommendations, "強化 Planner intake：子任務開始前先重述成功條件，若目標模糊則要求 Executor 先提出澄清問題。")
		case "missing_context":
			recommendations = append(recommendations, "在 Planner rules 補上 context checklist，強制附上關聯檔案、Issue 摘要、最近失敗訊息與驗證命令。")
		case "wrong_tool_hint":
			recommendations = append(recommendations, "調整 tool hints 範本：把搜尋、讀檔、建置、測試的推薦工具寫死到 prompt，減少模型自行猜工具。")
		case "underspecified_input":
			recommendations = append(recommendations, "要求 Planner 交付更可執行的輸入，至少包含修改範圍、限制條件、驗證方式與禁止觸碰區域。")
		case "missing_validation":
			recommendations = append(recommendations, "把驗證步驟列為必填欄位；若子任務產出程式碼但未跑對應測試或 build，Review 應直接標記 partial/fail。")
		default:
			recommendations = append(recommendations, fmt.Sprintf("檢查 `%s` 高頻原因，必要時把對應防呆規則前移到 Planner prompt。", tag.Tag))
		}
	}
	return recommendations
}

// FormatPlannerRulesWeeklyReport renders the weekly tuner report for Telegram/chat output.
func FormatPlannerRulesWeeklyReport(report PlannerRulesWeeklyReport) string {
	var b strings.Builder
	b.WriteString("📊 Hermes Review 週報（近 7 天）\n")
	b.WriteString(fmt.Sprintf("期間：%s ~ %s\n", report.WindowStart.Format("2006-01-02"), report.WindowEnd.Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("Review 數：%d | 子任務複審數：%d | 平均分：%.1f\n", report.ReviewCount, report.ReviewedSubTaskCount, report.AverageOverallScore))
	b.WriteString(fmt.Sprintf("Verdict：pass=%d partial=%d fail=%d\n",
		report.VerdictCounts["pass"], report.VerdictCounts["partial"], report.VerdictCounts["fail"]))

	b.WriteString("Top issue tags：")
	if len(report.TopIssueTags) == 0 {
		b.WriteString("無\n")
	} else {
		b.WriteString("\n")
		for i, tag := range report.TopIssueTags {
			b.WriteString(fmt.Sprintf("%d. %s (%d 次；task=%d, subtask=%d)\n", i+1, tag.Tag, tag.Count, tag.TaskCount, tag.SubTaskCount))
		}
	}

	b.WriteString("Planner 建議：\n")
	for i, rec := range report.Recommendations {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
	}
	return strings.TrimSpace(b.String())
}
