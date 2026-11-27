package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// hermesTaskSummary is one task of the exported history.
type hermesTaskSummary struct {
	TaskID            string          `json:"task_id"`
	ProjectDir        string          `json:"project_dir"`
	Goal              string          `json:"goal"`
	GithubIssueNumber int             `json:"github_issue_number,omitempty"`
	Status            string          `json:"status"`
	UsedTokens        int             `json:"used_tokens"`
	UpdatedAt         time.Time       `json:"updated_at"`
	SubTasks          []hermesSubTask `json:"sub_tasks"`
	Summary           string          `json:"summary"`
}

// writeHermesExport writes every historical task of db to w as a JSON array.
func writeHermesExport(db *sql.DB, w io.Writer) error {
	history := hermesHistory{db: db}
	if err := history.check(); err != nil {
		return err
	}
	out := []hermesTaskSummary{}
	for offset := 0; ; {
		refs, total, err := history.list(hermesHistoryFilter{Limit: 500, Offset: offset})
		if err != nil {
			return err
		}
		for _, ref := range refs {
			summary := hermesTaskSummary{
				TaskID: ref.TaskID, ProjectDir: ref.ProjectDir, Goal: ref.Goal,
				GithubIssueNumber: ref.GithubIssueNumber, Status: ref.Status,
				UsedTokens: ref.UsedTokens, UpdatedAt: ref.UpdatedAt, SubTasks: []hermesSubTask{},
			}
			var planJSON, accumulated, projectDir string
			if err := db.QueryRow(`SELECT plan_json, accumulated, project_dir FROM hermes_task_states WHERE id = ?`,
				ref.TaskID).Scan(&planJSON, &accumulated, &projectDir); err != nil {
				return fmt.Errorf("read hermes task %s: %w", ref.TaskID, err)
			}
			_ = json.Unmarshal([]byte(planJSON), &summary.SubTasks)
			summary.Summary = accumulated
			if summary.ProjectDir == "" {
				summary.ProjectDir = projectDir
			}
			// The latest snapshot is newer than the task row where both exist.
			var stateJSON string
			err := db.QueryRow(`SELECT state_json FROM hermes_snapshots WHERE task_id = ? ORDER BY step DESC LIMIT 1`,
				ref.TaskID).Scan(&stateJSON)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("read hermes snapshot of %s: %w", ref.TaskID, err)
			}
			var state hermesSnapshotState
			if err == nil && json.Unmarshal([]byte(stateJSON), &state) == nil {
				if len(state.Plan) > 0 {
					summary.SubTasks = state.Plan
				}
				if state.Accumulated != "" {
					summary.Summary = state.Accumulated
				}
				if state.Status != "" {
					summary.Status = state.Status
				}
			}
			out = append(out, summary)
		}
		offset += len(refs)
		if len(refs) == 0 || offset >= total {
			break
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// exportHermesHistory runs `alice export-hermes-history [database]`. It opens
// the database read-only, so it is safe beside a running service.
func exportHermesHistory(args []string, w io.Writer) error {
	path := "./data/alice.db"
	if len(args) > 0 {
		path = args[0]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String())
	if err != nil {
		return err
	}
	defer db.Close()
	return writeHermesExport(db, w)
}
