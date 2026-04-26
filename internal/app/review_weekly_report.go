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
func (s *SQLiteStorage) GetPlannerRulesWeeklyReport(windowStart, windowEnd time.Time, i18n *I18nManager, lang string) (PlannerRulesWeeklyReport, error) {
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
	report.Recommendations = plannerRuleRecommendations(i18n, lang, report.TopIssueTags, report.ReviewCount)
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

func plannerRuleRecommendations(i18n *I18nManager, lang string, tags []PlannerRulesIssueTagStat, reviewCount int) []string {
	if reviewCount == 0 {
		return []string{plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_no_reviews", nil)}
	}

	recommendations := make([]string, 0, len(tags)+1)
	if reviewCount < 10 {
		recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_small_sample", map[string]string{
			"review_count": fmt.Sprintf("%d", reviewCount),
		}))
	}
	for _, tag := range tags {
		switch tag.Tag {
		case "ambiguous_goal":
			recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_rec_ambiguous_goal", nil))
		case "missing_context":
			recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_rec_missing_context", nil))
		case "wrong_tool_hint":
			recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_rec_wrong_tool_hint", nil))
		case "underspecified_input":
			recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_rec_underspecified_input", nil))
		case "missing_validation":
			recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_rec_missing_validation", nil))
		default:
			recommendations = append(recommendations, plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_rec_generic", map[string]string{
				"tag": tag.Tag,
			}))
		}
	}
	return recommendations
}

// FormatPlannerRulesWeeklyReport renders the weekly tuner report for Telegram/chat output.
func FormatPlannerRulesWeeklyReport(i18n *I18nManager, lang string, report PlannerRulesWeeklyReport) string {
	var b strings.Builder
	b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_title", nil))
	b.WriteString("\n")
	b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_period", map[string]string{
		"start": report.WindowStart.Format("2006-01-02"),
		"end":   report.WindowEnd.Format("2006-01-02"),
	}))
	b.WriteString("\n")
	b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_summary", map[string]string{
		"review_count":  fmt.Sprintf("%d", report.ReviewCount),
		"subtask_count": fmt.Sprintf("%d", report.ReviewedSubTaskCount),
		"average_score": fmt.Sprintf("%.1f", report.AverageOverallScore),
	}))
	b.WriteString("\n")
	b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_verdicts", map[string]string{
		"pass":    fmt.Sprintf("%d", report.VerdictCounts["pass"]),
		"partial": fmt.Sprintf("%d", report.VerdictCounts["partial"]),
		"fail":    fmt.Sprintf("%d", report.VerdictCounts["fail"]),
	}))
	b.WriteString("\n")

	b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_top_issue_tags_title", nil))
	if len(report.TopIssueTags) == 0 {
		b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_no_top_issue_tags", nil))
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
		for i, tag := range report.TopIssueTags {
			b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_issue_tag_item", map[string]string{
				"index":         fmt.Sprintf("%d", i+1),
				"tag":           tag.Tag,
				"count":         fmt.Sprintf("%d", tag.Count),
				"task_count":    fmt.Sprintf("%d", tag.TaskCount),
				"subtask_count": fmt.Sprintf("%d", tag.SubTaskCount),
			}))
			b.WriteString("\n")
		}
	}

	b.WriteString(plannerWeeklyReportMessage(i18n, lang, "review_weekly_report_recommendations_title", nil))
	b.WriteString("\n")
	for i, rec := range report.Recommendations {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
	}
	return strings.TrimSpace(b.String())
}

func plannerWeeklyReportMessage(i18n *I18nManager, lang, key string, vars map[string]string) string {
	if i18n == nil {
		return key
	}
	return i18n.GetMessage(lang, key, vars)
}
