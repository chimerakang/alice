package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleMemoryPreviewReturnsGeneralTaskSections(t *testing.T) {
	storage := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = storage
	t.Cleanup(func() { globalStorage = oldStorage })

	if err := storage.InsertDecisionLog(DecisionLog{
		Timestamp:     time.Now().Add(-time.Hour),
		SessionID:     "memory-preview-session",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "整理 #143 memory preview API",
		AgentResponse: "Memory preview API 會回傳 section source、scope、size 與 preview。",
		Outcome: ExecutionOutcome{
			Success:  true,
			TaskType: "analysis",
		},
		Model: "gpt-5.5",
	}); err != nil {
		t.Fatalf("InsertDecisionLog: %v", err)
	}

	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodGet, "/api/memory/preview?chat_id=42&thread_id=7&project_dir=/repo&issue=143&message=繼續", nil)
	w := httptest.NewRecorder()
	wi.handleMemoryPreview(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		SectionCount int `json:"section_count"`
		Sections     []struct {
			Source  string `json:"source"`
			Scope   string `json:"scope"`
			Size    int    `json:"size"`
			Preview string `json:"preview"`
		} `json:"sections"`
		RenderedPreview string `json:"rendered_preview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SectionCount != 1 || len(payload.Sections) != 1 {
		t.Fatalf("sections = %+v, count=%d", payload.Sections, payload.SectionCount)
	}
	section := payload.Sections[0]
	if section.Source != "general_task" {
		t.Fatalf("source = %q, want general_task", section.Source)
	}
	if section.Scope != "issue:143" {
		t.Fatalf("scope = %q, want issue:143", section.Scope)
	}
	if section.Size == 0 || !strings.Contains(section.Preview, "Continuation hints") {
		t.Fatalf("unexpected preview section: %+v", section)
	}
	if !strings.Contains(payload.RenderedPreview, "Continuation hints") {
		t.Fatalf("rendered preview missing memory content: %q", payload.RenderedPreview)
	}
}

func TestParseMemoryPreviewRequestRequiresChatID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/memory/preview", nil)
	if _, err := parseMemoryPreviewRequest(req); err == nil {
		t.Fatal("expected missing chat_id error")
	}
}
