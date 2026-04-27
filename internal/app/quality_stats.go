package app

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type QualityWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Label string    `json:"label"`
}

type QualityBucket struct {
	Label       string  `json:"label"`
	Count       int     `json:"count"`
	Percentage  float64 `json:"percentage"`
	AvgScore    float64 `json:"avg_score"`
	PartialRate float64 `json:"partial_rate"`
	FailRate    float64 `json:"fail_rate"`
}

type QualityTrendPoint struct {
	Period      string  `json:"period"`
	TaskCount   int     `json:"task_count"`
	ReviewCount int     `json:"review_count"`
	AvgSubTasks float64 `json:"avg_sub_tasks"`
	PassRate    float64 `json:"pass_rate"`
	PartialRate float64 `json:"partial_rate"`
	FailRate    float64 `json:"fail_rate"`
	AvgScore    float64 `json:"avg_score"`
	AvgSubScore float64 `json:"avg_sub_score"`
}

type QualityGranularityScore struct {
	SubTaskCount int     `json:"sub_task_count"`
	TaskCount    int     `json:"task_count"`
	AvgScore     float64 `json:"avg_score"`
}

type QualityToolHintStat struct {
	ToolHints string  `json:"tool_hints"`
	Count     int     `json:"count"`
	PassRate  float64 `json:"pass_rate"`
	AvgScore  float64 `json:"avg_score"`
}

type QualityDecompositionStats struct {
	WindowStart        time.Time                 `json:"window_start"`
	WindowEnd          time.Time                 `json:"window_end"`
	TaskCount          int                       `json:"task_count"`
	SubTaskCount       int                       `json:"sub_task_count"`
	AvgSubTasks        float64                   `json:"avg_sub_tasks"`
	StdDevSubTasks     float64                   `json:"stddev_sub_tasks"`
	BestGranularity    string                    `json:"best_granularity"`
	GranularityBuckets []QualityBucket           `json:"granularity_buckets"`
	GranularityScores  []QualityGranularityScore `json:"granularity_scores"`
	WeeklyTrend        []QualityTrendPoint       `json:"weekly_trend"`
	DescriptionBuckets []QualityBucket           `json:"description_buckets"`
	ToolHintStats      []QualityToolHintStat     `json:"tool_hint_stats"`
}

type QualityIssueTagStat struct {
	Tag           string `json:"tag"`
	Count         int    `json:"count"`
	PreviousCount int    `json:"previous_count"`
	Delta         int    `json:"delta"`
	Trend         string `json:"trend"`
}

type QualityLowScoringSubTask struct {
	TaskID      string    `json:"task_id"`
	SubTaskID   string    `json:"sub_task_id"`
	Description string    `json:"description"`
	Score       int       `json:"score"`
	IssueTags   []string  `json:"issue_tags"`
	Feedback    string    `json:"feedback"`
	CreatedAt   time.Time `json:"created_at"`
}

type QualityScoreStats struct {
	WindowStart          time.Time                  `json:"window_start"`
	WindowEnd            time.Time                  `json:"window_end"`
	ReviewCount          int                        `json:"review_count"`
	ReviewedSubTaskCount int                        `json:"reviewed_sub_task_count"`
	PassRate             float64                    `json:"pass_rate"`
	PartialRate          float64                    `json:"partial_rate"`
	FailRate             float64                    `json:"fail_rate"`
	AvgOverallScore      float64                    `json:"avg_overall_score"`
	AvgSubTaskScore      float64                    `json:"avg_sub_task_score"`
	VerdictDistribution  map[string]int             `json:"verdict_distribution"`
	Trend                []QualityTrendPoint        `json:"trend"`
	TopIssueTags         []QualityIssueTagStat      `json:"top_issue_tags"`
	LowScoringSubTasks   []QualityLowScoringSubTask `json:"low_scoring_sub_tasks"`
}

type QualityInsight struct {
	Name       string  `json:"name"`
	Severity   string  `json:"severity"`
	Message    string  `json:"message"`
	Suggestion string  `json:"suggestion"`
	Value      float64 `json:"value"`
	Threshold  float64 `json:"threshold"`
}

type qualityTaskRow struct {
	ID        string
	Goal      string
	SubTasks  int
	AvgScore  float64
	Verdict   string
	CreatedAt time.Time
}

func ResolveQualityWindow(value string, now time.Time) (QualityWindow, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		value = "30d"
	}
	days := 30
	switch value {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		return QualityWindow{}, fmt.Errorf("invalid window %q; use 7d, 30d, or 90d", value)
	}
	end := now.UTC()
	return QualityWindow{Start: end.Add(-time.Duration(days) * 24 * time.Hour), End: end, Label: value}, nil
}

func (s *SQLiteStorage) GetQualityDecompositionStats(window QualityWindow) (QualityDecompositionStats, error) {
	window = normalizeQualityWindow(window)
	stats := QualityDecompositionStats{WindowStart: window.Start, WindowEnd: window.End}
	rows, err := s.db.Query(`
		SELECT t.id, COUNT(st.id) AS sub_count, COALESCE(rr.overall_score, 0), COALESCE(rr.verdict, ''), COALESCE(rr.created_at, t.started_at)
		FROM tasks t
		LEFT JOIN sub_tasks st ON st.task_id = t.id
		LEFT JOIN review_results rr ON rr.id = (
			SELECT id FROM review_results WHERE task_id = t.id ORDER BY created_at DESC, id DESC LIMIT 1
		)
		WHERE t.started_at >= ? AND t.started_at <= ?
		GROUP BY t.id
		ORDER BY t.started_at ASC`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	)
	if err != nil {
		return stats, fmt.Errorf("query quality decomposition tasks: %w", err)
	}
	defer rows.Close()

	counts := make([]int, 0)
	buckets := map[string]*QualityBucket{
		"1-3":  {Label: "1-3"},
		"4-7":  {Label: "4-7"},
		"8-15": {Label: "8-15"},
	}
	scoreByCount := map[int]*QualityGranularityScore{}
	weekly := map[string]*QualityTrendPoint{}
	scoreSums := map[string]float64{}
	scoreCounts := map[string]int{}
	bestLabel := ""
	bestScore := -1.0

	for rows.Next() {
		var id, verdict, createdAtRaw string
		var subCount, score int
		if err := rows.Scan(&id, &subCount, &score, &verdict, &createdAtRaw); err != nil {
			return stats, fmt.Errorf("scan quality decomposition task: %w", err)
		}
		_ = id
		stats.TaskCount++
		stats.SubTaskCount += subCount
		counts = append(counts, subCount)

		label := granularityLabel(subCount)
		if _, ok := buckets[label]; !ok {
			buckets[label] = &QualityBucket{Label: label}
		}
		b := buckets[label]
		b.Count++
		if strings.EqualFold(verdict, "partial") {
			b.PartialRate++
		}
		if strings.EqualFold(verdict, "fail") {
			b.FailRate++
		}
		if score > 0 {
			scoreSums[label] += float64(score)
			scoreCounts[label]++
		}

		if subCount > 0 {
			gs := scoreByCount[subCount]
			if gs == nil {
				gs = &QualityGranularityScore{SubTaskCount: subCount}
				scoreByCount[subCount] = gs
			}
			gs.TaskCount++
			if score > 0 {
				gs.AvgScore += float64(score)
			}
		}

		period := qualityWeek(parseDBTime(createdAtRaw))
		tp := weekly[period]
		if tp == nil {
			tp = &QualityTrendPoint{Period: period}
			weekly[period] = tp
		}
		tp.TaskCount++
		tp.AvgSubTasks += float64(subCount)
	}
	if err := rows.Err(); err != nil {
		return stats, fmt.Errorf("iterate quality decomposition tasks: %w", err)
	}

	if stats.TaskCount > 0 {
		stats.AvgSubTasks = float64(stats.SubTaskCount) / float64(stats.TaskCount)
		stats.StdDevSubTasks = stddevInts(counts, stats.AvgSubTasks)
	}
	for label, b := range buckets {
		if stats.TaskCount > 0 {
			b.Percentage = float64(b.Count) / float64(stats.TaskCount) * 100
		}
		if b.Count > 0 {
			b.PartialRate = b.PartialRate / float64(b.Count) * 100
			b.FailRate = b.FailRate / float64(b.Count) * 100
		}
		if scoreCounts[label] > 0 {
			b.AvgScore = scoreSums[label] / float64(scoreCounts[label])
			if b.AvgScore > bestScore {
				bestScore = b.AvgScore
				bestLabel = label
			}
		}
	}
	stats.BestGranularity = bestLabel
	stats.GranularityBuckets = orderedQualityBuckets(buckets, []string{"1-3", "4-7", "8-15", "16+"})
	stats.GranularityScores = finishGranularityScores(scoreByCount)
	stats.WeeklyTrend = finishTrendPoints(weekly)

	descBuckets, err := s.qualityDescriptionBuckets(window)
	if err != nil {
		return stats, err
	}
	stats.DescriptionBuckets = descBuckets
	toolStats, err := s.qualityToolHintStats(window)
	if err != nil {
		return stats, err
	}
	stats.ToolHintStats = toolStats
	return stats, nil
}

func (s *SQLiteStorage) GetQualityScoreStats(window QualityWindow) (QualityScoreStats, error) {
	window = normalizeQualityWindow(window)
	stats := QualityScoreStats{
		WindowStart:         window.Start,
		WindowEnd:           window.End,
		VerdictDistribution: map[string]int{"pass": 0, "partial": 0, "fail": 0},
	}
	rows, err := s.db.Query(`
		SELECT id, verdict, overall_score, issue_tags, created_at
		FROM review_results
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at ASC`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	)
	if err != nil {
		return stats, fmt.Errorf("query quality scores: %w", err)
	}
	defer rows.Close()

	tagCounts := map[string]int{}
	trend := map[string]*QualityTrendPoint{}
	totalScore := 0
	for rows.Next() {
		var reviewID int64
		var verdict, rawTags, createdAtRaw string
		var score int
		if err := rows.Scan(&reviewID, &verdict, &score, &rawTags, &createdAtRaw); err != nil {
			return stats, fmt.Errorf("scan quality score: %w", err)
		}
		_ = reviewID
		verdict = normalizeVerdict(verdict)
		stats.ReviewCount++
		stats.VerdictDistribution[verdict]++
		totalScore += score
		for _, tag := range decodeIssueTags(rawTags) {
			tagCounts[tag]++
		}
		period := qualityDay(parseDBTime(createdAtRaw))
		tp := trend[period]
		if tp == nil {
			tp = &QualityTrendPoint{Period: period}
			trend[period] = tp
		}
		tp.ReviewCount++
		tp.AvgScore += float64(score)
		switch verdict {
		case "pass":
			tp.PassRate++
		case "partial":
			tp.PartialRate++
		case "fail":
			tp.FailRate++
		}
	}
	if err := rows.Err(); err != nil {
		return stats, fmt.Errorf("iterate quality scores: %w", err)
	}
	if stats.ReviewCount > 0 {
		stats.AvgOverallScore = float64(totalScore) / float64(stats.ReviewCount)
		stats.PassRate = float64(stats.VerdictDistribution["pass"]) / float64(stats.ReviewCount) * 100
		stats.PartialRate = float64(stats.VerdictDistribution["partial"]) / float64(stats.ReviewCount) * 100
		stats.FailRate = float64(stats.VerdictDistribution["fail"]) / float64(stats.ReviewCount) * 100
	}

	subCount, subAvg, err := s.qualitySubTaskScoreSummary(window)
	if err != nil {
		return stats, err
	}
	stats.ReviewedSubTaskCount = subCount
	stats.AvgSubTaskScore = subAvg
	stats.Trend = finishScoreTrendPoints(trend)
	previous, err := s.qualityPreviousIssueTagCounts(window)
	if err != nil {
		return stats, err
	}
	stats.TopIssueTags = topQualityTags(tagCounts, previous, 5)
	low, err := s.qualityLowScoringSubTasks(window)
	if err != nil {
		return stats, err
	}
	stats.LowScoringSubTasks = low
	return stats, nil
}

func (s *SQLiteStorage) GetQualityInsights(window QualityWindow) ([]QualityInsight, error) {
	window = normalizeQualityWindow(window)
	decomp, err := s.GetQualityDecompositionStats(window)
	if err != nil {
		return nil, err
	}
	scores, err := s.GetQualityScoreStats(window)
	if err != nil {
		return nil, err
	}
	insights := make([]QualityInsight, 0, 8)
	if scores.ReviewCount == 0 {
		return []QualityInsight{{
			Name: "no_recent_reviews", Severity: "info", Message: "目前視窗內沒有 review 樣本",
			Suggestion: "先累積 review_results 後再判斷拆分品質", Threshold: 1,
		}}, nil
	}
	if scores.PartialRate >= 35 {
		insights = append(insights, QualityInsight{Name: "high_partial_rate", Severity: "warning", Message: fmt.Sprintf("partial 率 %.0f%% 高於目標", scores.PartialRate), Suggestion: "檢查 Planner rules 是否需要更明確的驗收條件，並評估 #124 strict mode", Value: scores.PartialRate, Threshold: 35})
	}
	for _, b := range decomp.GranularityBuckets {
		if b.Label == "8-15" && b.PartialRate >= 60 && b.Count > 0 {
			insights = append(insights, QualityInsight{Name: "too_many_subtasks_partial", Severity: "warning", Message: fmt.Sprintf("8+ sub-task 任務 partial 率 %.0f%%", b.PartialRate), Suggestion: "大型任務應拆成多個 issue，或收斂每個 sub-task 的驗收範圍", Value: b.PartialRate, Threshold: 60})
		}
	}
	for _, b := range decomp.DescriptionBuckets {
		if b.Label == "<50" && b.FailRate >= 25 && b.Count > 0 {
			insights = append(insights, QualityInsight{Name: "short_description_fail_rate", Severity: "warning", Message: fmt.Sprintf("描述少於 50 字的 sub-task fail 率 %.0f%%", b.FailRate), Suggestion: "Planner rules 可加入最小描述長度與輸入條件要求", Value: b.FailRate, Threshold: 25})
		}
	}
	if len(scores.TopIssueTags) > 0 && scores.TopIssueTags[0].Count >= 3 {
		top := scores.TopIssueTags[0]
		insights = append(insights, QualityInsight{Name: "top_issue_tag", Severity: "info", Message: fmt.Sprintf("最高頻失分標籤是 %s（%d 次）", top.Tag, top.Count), Suggestion: "把此標籤對應的修正規則加入 Planner weekly tuning", Value: float64(top.Count), Threshold: 3})
	}
	for _, tool := range decomp.ToolHintStats {
		if tool.Count >= 3 && tool.PassRate >= 90 {
			insights = append(insights, QualityInsight{Name: "high_pass_tool_hints", Severity: "success", Message: fmt.Sprintf("%s 的 pass 率 %.0f%%", tool.ToolHints, tool.PassRate), Suggestion: "可作為相似任務的 tool_hints 範本", Value: tool.PassRate, Threshold: 90})
			break
		}
	}
	missingModelRate, err := s.qualityMissingModelRate(window)
	if err != nil {
		return nil, err
	}
	if missingModelRate >= 50 {
		insights = append(insights, QualityInsight{Name: "missing_subtask_model", Severity: "warning", Message: fmt.Sprintf("未指定 model 的 sub-task 比例 %.0f%%", missingModelRate), Suggestion: "複雜任務應由 Planner 明確指定 model 或 routing reason", Value: missingModelRate, Threshold: 50})
	}
	riskRate, err := s.qualityRiskVerbPartialRate(window)
	if err != nil {
		return nil, err
	}
	if riskRate >= 60 {
		insights = append(insights, QualityInsight{Name: "risk_verbs_partial_rate", Severity: "warning", Message: fmt.Sprintf("含 commit/push/deploy 的任務 partial 率 %.0f%%", riskRate), Suggestion: "建議啟用 #124 strict mode 自動觸發", Value: riskRate, Threshold: 60})
	}
	if len(insights) == 0 {
		insights = append(insights, QualityInsight{Name: "quality_stable", Severity: "success", Message: "目前沒有命中高風險拆分規則", Suggestion: "維持現有 Planner rules，持續累積樣本", Value: scores.PassRate, Threshold: 0})
	}
	return insights, nil
}

func normalizeQualityWindow(window QualityWindow) QualityWindow {
	if window.End.IsZero() {
		window.End = time.Now().UTC()
	}
	if window.Start.IsZero() {
		window.Start = window.End.Add(-30 * 24 * time.Hour)
	}
	return QualityWindow{Start: window.Start.UTC(), End: window.End.UTC(), Label: window.Label}
}

func granularityLabel(count int) string {
	switch {
	case count <= 3:
		return "1-3"
	case count <= 7:
		return "4-7"
	case count <= 15:
		return "8-15"
	default:
		return "16+"
	}
}

func normalizeVerdict(verdict string) string {
	verdict = strings.ToLower(strings.TrimSpace(verdict))
	if verdict == "pass" || verdict == "partial" || verdict == "fail" {
		return verdict
	}
	return "unknown"
}

func stddevInts(values []int, avg float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		delta := float64(value) - avg
		sum += delta * delta
	}
	return math.Sqrt(sum / float64(len(values)))
}

func orderedQualityBuckets(buckets map[string]*QualityBucket, order []string) []QualityBucket {
	out := make([]QualityBucket, 0, len(buckets))
	seen := map[string]bool{}
	for _, label := range order {
		if b := buckets[label]; b != nil {
			out = append(out, *b)
			seen[label] = true
		}
	}
	for label, b := range buckets {
		if !seen[label] {
			out = append(out, *b)
		}
	}
	return out
}

func finishGranularityScores(values map[int]*QualityGranularityScore) []QualityGranularityScore {
	out := make([]QualityGranularityScore, 0, len(values))
	for _, value := range values {
		if value.TaskCount > 0 {
			value.AvgScore = value.AvgScore / float64(value.TaskCount)
		}
		out = append(out, *value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubTaskCount < out[j].SubTaskCount })
	return out
}

func qualityWeek(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func qualityDay(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.Format("01-02")
}

func finishTrendPoints(values map[string]*QualityTrendPoint) []QualityTrendPoint {
	out := make([]QualityTrendPoint, 0, len(values))
	for _, value := range values {
		if value.TaskCount > 0 {
			value.AvgSubTasks = value.AvgSubTasks / float64(value.TaskCount)
		}
		out = append(out, *value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Period < out[j].Period })
	return out
}

func finishScoreTrendPoints(values map[string]*QualityTrendPoint) []QualityTrendPoint {
	out := make([]QualityTrendPoint, 0, len(values))
	for _, value := range values {
		if value.ReviewCount > 0 {
			value.AvgScore = value.AvgScore / float64(value.ReviewCount)
			value.PassRate = value.PassRate / float64(value.ReviewCount) * 100
			value.PartialRate = value.PartialRate / float64(value.ReviewCount) * 100
			value.FailRate = value.FailRate / float64(value.ReviewCount) * 100
		}
		out = append(out, *value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Period < out[j].Period })
	return out
}

func (s *SQLiteStorage) qualityDescriptionBuckets(window QualityWindow) ([]QualityBucket, error) {
	rows, err := s.db.Query(`
		SELECT
			CASE WHEN length(st.description) < 50 THEN '<50'
			     WHEN length(st.description) <= 200 THEN '50-200'
			     ELSE '200+' END AS bucket,
			COUNT(*),
			AVG(COALESCE(rs.score, 0)),
			COALESCE(SUM(CASE WHEN COALESCE(rr.verdict, '') <> '' AND COALESCE(rr.verdict, '') <> 'pass' THEN 1 ELSE 0 END), 0)
		FROM sub_tasks st
		JOIN tasks t ON t.id = st.task_id
		LEFT JOIN review_results rr ON rr.id = (
			SELECT id FROM review_results WHERE task_id = t.id ORDER BY created_at DESC, id DESC LIMIT 1
		)
		LEFT JOIN review_subtask_results rs ON rs.review_id = rr.id AND rs.sub_task_id = st.id
		WHERE t.started_at >= ? AND t.started_at <= ?
		GROUP BY bucket`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("query quality description buckets: %w", err)
	}
	defer rows.Close()
	buckets := map[string]*QualityBucket{}
	total := 0
	for rows.Next() {
		var label string
		var count, failCount int
		var avg sql.NullFloat64
		if err := rows.Scan(&label, &count, &avg, &failCount); err != nil {
			return nil, fmt.Errorf("scan quality description bucket: %w", err)
		}
		total += count
		b := &QualityBucket{Label: label, Count: count}
		if avg.Valid {
			b.AvgScore = avg.Float64
		}
		if count > 0 {
			b.FailRate = float64(failCount) / float64(count) * 100
		}
		buckets[label] = b
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quality description buckets: %w", err)
	}
	for _, b := range buckets {
		if total > 0 {
			b.Percentage = float64(b.Count) / float64(total) * 100
		}
	}
	return orderedQualityBuckets(buckets, []string{"<50", "50-200", "200+"}), nil
}

func (s *SQLiteStorage) qualityToolHintStats(window QualityWindow) ([]QualityToolHintStat, error) {
	rows, err := s.db.Query(`
		SELECT hints, COUNT(*), AVG(score), SUM(CASE WHEN verdict = 'pass' THEN 1 ELSE 0 END)
		FROM (
			SELECT t.id,
			       COALESCE((SELECT group_concat(tool_name, '+') FROM (
			          SELECT DISTINCT te.tool_name
			          FROM tool_events te
			          JOIN sub_tasks sts ON sts.id = te.sub_task_id
			          WHERE sts.task_id = t.id AND te.tool_name <> ''
			          ORDER BY te.tool_name
			       )), 'none') AS hints,
			       COALESCE(rr.overall_score, 0) AS score,
			       COALESCE(rr.verdict, '') AS verdict
			FROM tasks t
			LEFT JOIN review_results rr ON rr.id = (
				SELECT id FROM review_results WHERE task_id = t.id ORDER BY created_at DESC, id DESC LIMIT 1
			)
			WHERE t.started_at >= ? AND t.started_at <= ?
		)
		GROUP BY hints
		ORDER BY COUNT(*) DESC, AVG(score) DESC
		LIMIT 8`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("query quality tool hints: %w", err)
	}
	defer rows.Close()
	var out []QualityToolHintStat
	for rows.Next() {
		var stat QualityToolHintStat
		var passCount int
		if err := rows.Scan(&stat.ToolHints, &stat.Count, &stat.AvgScore, &passCount); err != nil {
			return nil, fmt.Errorf("scan quality tool hints: %w", err)
		}
		if stat.Count > 0 {
			stat.PassRate = float64(passCount) / float64(stat.Count) * 100
		}
		out = append(out, stat)
	}
	return out, rows.Err()
}

func (s *SQLiteStorage) qualitySubTaskScoreSummary(window QualityWindow) (int, float64, error) {
	var count int
	var avg sql.NullFloat64
	err := s.db.QueryRow(`
		SELECT COUNT(*), AVG(rs.score)
		FROM review_subtask_results rs
		JOIN review_results rr ON rr.id = rs.review_id
		WHERE rr.created_at >= ? AND rr.created_at <= ?`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	).Scan(&count, &avg)
	if err != nil {
		return 0, 0, fmt.Errorf("query quality subtask score summary: %w", err)
	}
	if !avg.Valid {
		return count, 0, nil
	}
	return count, avg.Float64, nil
}

func (s *SQLiteStorage) qualityPreviousIssueTagCounts(window QualityWindow) (map[string]int, error) {
	duration := window.End.Sub(window.Start)
	prevStart := window.Start.Add(-duration)
	rows, err := s.db.Query(`
		SELECT issue_tags FROM review_results
		WHERE created_at >= ? AND created_at < ?`,
		prevStart.Format(time.RFC3339Nano), window.Start.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("query previous quality issue tags: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan previous quality issue tags: %w", err)
		}
		for _, tag := range decodeIssueTags(raw) {
			counts[tag]++
		}
	}
	return counts, rows.Err()
}

func topQualityTags(current, previous map[string]int, limit int) []QualityIssueTagStat {
	stats := make([]QualityIssueTagStat, 0, len(current))
	for tag, count := range current {
		prev := previous[tag]
		trend := "flat"
		if count > prev {
			trend = "up"
		} else if count < prev {
			trend = "down"
		}
		stats = append(stats, QualityIssueTagStat{Tag: tag, Count: count, PreviousCount: prev, Delta: count - prev, Trend: trend})
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

func (s *SQLiteStorage) qualityLowScoringSubTasks(window QualityWindow) ([]QualityLowScoringSubTask, error) {
	rows, err := s.db.Query(`
		SELECT rr.task_id, rs.sub_task_id, st.description, rs.score, rs.issue_tags, rs.feedback, rr.created_at
		FROM review_subtask_results rs
		JOIN review_results rr ON rr.id = rs.review_id
		JOIN sub_tasks st ON st.id = rs.sub_task_id
		WHERE rr.created_at >= ? AND rr.created_at <= ?
		ORDER BY rs.score ASC, rr.created_at DESC
		LIMIT 8`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("query low scoring subtasks: %w", err)
	}
	defer rows.Close()
	out := make([]QualityLowScoringSubTask, 0, 8)
	for rows.Next() {
		var item QualityLowScoringSubTask
		var tagsRaw, createdAtRaw string
		if err := rows.Scan(&item.TaskID, &item.SubTaskID, &item.Description, &item.Score, &tagsRaw, &item.Feedback, &createdAtRaw); err != nil {
			return nil, fmt.Errorf("scan low scoring subtask: %w", err)
		}
		item.IssueTags = decodeIssueTags(tagsRaw)
		item.CreatedAt = parseDBTime(createdAtRaw)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStorage) qualityMissingModelRate(window QualityWindow) (float64, error) {
	var total, missing int
	err := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN trim(model) = '' THEN 1 ELSE 0 END), 0)
		FROM sub_tasks
		WHERE started_at >= ? AND started_at <= ?`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	).Scan(&total, &missing)
	if err != nil {
		return 0, fmt.Errorf("query missing model rate: %w", err)
	}
	if total == 0 {
		return 0, nil
	}
	return float64(missing) / float64(total) * 100, nil
}

func (s *SQLiteStorage) qualityRiskVerbPartialRate(window QualityWindow) (float64, error) {
	var total, partial int
	err := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN COALESCE(rr.verdict, '') = 'partial' THEN 1 ELSE 0 END), 0)
		FROM tasks t
		LEFT JOIN review_results rr ON rr.id = (
			SELECT id FROM review_results WHERE task_id = t.id ORDER BY created_at DESC, id DESC LIMIT 1
		)
		WHERE t.started_at >= ? AND t.started_at <= ?
		  AND (lower(t.goal) LIKE '%commit%' OR lower(t.goal) LIKE '%push%' OR lower(t.goal) LIKE '%deploy%')`,
		window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano),
	).Scan(&total, &partial)
	if err != nil {
		return 0, fmt.Errorf("query risk verb partial rate: %w", err)
	}
	if total == 0 {
		return 0, nil
	}
	return float64(partial) / float64(total) * 100, nil
}
