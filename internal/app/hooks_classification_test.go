package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHandlePromptClassifications_NoStorage verifies the handler returns an
// empty array when globalStorage is nil (cold-start scenario).
func TestHandlePromptClassifications_NoStorage(t *testing.T) {
	old := globalStorage
	globalStorage = nil
	defer func() { globalStorage = old }()

	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodGet, "/api/hooks/prompt-classifications", nil)
	w := httptest.NewRecorder()
	wi.handlePromptClassifications(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var recs []PromptClassificationRecord
	if err := json.NewDecoder(w.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected empty array, got %d records", len(recs))
	}
}

// TestHandlePromptClassifications_MethodNotAllowed verifies POST is rejected.
func TestHandlePromptClassifications_MethodNotAllowed(t *testing.T) {
	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/prompt-classifications", nil)
	w := httptest.NewRecorder()
	wi.handlePromptClassifications(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestHandlePromptClassifications_WithRecords exercises the happy path:
// records inserted in storage are returned in the HTTP response.
func TestHandlePromptClassifications_WithRecords(t *testing.T) {
	s := newTestSQLiteStorage(t)
	old := globalStorage
	globalStorage = s
	defer func() { globalStorage = old }()

	base := time.Now().UTC()
	for _, rec := range []PromptClassificationRecord{
		{SessionID: "a1", Timestamp: base, Source: "vscode", Classification: "complex", MatchedRule: "refactor", PromptLength: 10},
		{SessionID: "a2", Timestamp: base.Add(time.Second), Source: "terminal", Classification: "trivial", PromptLength: 5},
	} {
		if err := s.RecordPromptClassification(rec); err != nil {
			t.Fatalf("RecordPromptClassification: %v", err)
		}
	}

	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodGet, "/api/hooks/prompt-classifications", nil)
	w := httptest.NewRecorder()
	wi.handlePromptClassifications(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var recs []PromptClassificationRecord
	if err := json.NewDecoder(w.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	// Newest first — a2 has later timestamp
	if recs[0].SessionID != "a2" {
		t.Errorf("expected a2 first, got %s", recs[0].SessionID)
	}
}

// TestHandlePromptClassifications_LimitQueryParam verifies the ?limit= param is honoured.
func TestHandlePromptClassifications_LimitQueryParam(t *testing.T) {
	s := newTestSQLiteStorage(t)
	old := globalStorage
	globalStorage = s
	defer func() { globalStorage = old }()

	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if err := s.RecordPromptClassification(PromptClassificationRecord{
			SessionID:      "lim" + string(rune('0'+i)),
			Timestamp:      base.Add(time.Duration(i) * time.Second),
			Classification: "trivial",
			PromptLength:   4,
		}); err != nil {
			t.Fatalf("RecordPromptClassification: %v", err)
		}
	}

	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodGet, "/api/hooks/prompt-classifications?limit=2", nil)
	w := httptest.NewRecorder()
	wi.handlePromptClassifications(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var recs []PromptClassificationRecord
	if err := json.NewDecoder(w.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records (limit=2), got %d", len(recs))
	}
}
