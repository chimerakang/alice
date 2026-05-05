package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- mock Client for prototype tests ----

type protoMockClient struct {
	response string
	err      error
}

func (m *protoMockClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &CLIResponse{Result: m.response, TextContent: m.response, SessionID: sessionID}, nil
}

func (m *protoMockClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(string, map[string]interface{}), onContent func(string, string)) (*CLIResponse, error) {
	return m.Call(ctx, message, projectDir, sessionID, modelOverride)
}

func (m *protoMockClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(string, string)) (*CLIResponse, error) {
	return m.Call(ctx, message, projectDir, sessionID, modelOverride)
}

func (m *protoMockClient) GetModel() string { return "test-model" }

// ---- helpers ----

func newTestPrototypeManager(t *testing.T, htmlResp string) (*PrototypeManager, *SQLiteStorage) {
	t.Helper()
	dir := t.TempDir()
	storage := openTestStorage(t)
	client := &protoMockClient{response: htmlResp}
	pm, err := NewPrototypeManager(client, dir, storage)
	if err != nil {
		t.Fatalf("NewPrototypeManager: %v", err)
	}
	return pm, storage
}

const sampleHTML = `<!DOCTYPE html><html><head></head><body><h1>Test</h1></body></html>`

// ---- extractHTML unit tests ----

func TestExtractHTML_CodeBlock(t *testing.T) {
	input := "here is the result:\n```html\n" + sampleHTML + "\n```\nDone."
	got := extractHTML(input)
	if got != sampleHTML {
		t.Errorf("extractHTML(codeBlock) = %q, want %q", got, sampleHTML)
	}
}

func TestExtractHTML_BareDoctype(t *testing.T) {
	got := extractHTML(sampleHTML)
	if got != sampleHTML {
		t.Errorf("extractHTML(bareDoctype) = %q, want %q", got, sampleHTML)
	}
}

func TestExtractHTML_Fallback(t *testing.T) {
	input := "no html here"
	got := extractHTML(input)
	if got != input {
		t.Errorf("extractHTML(fallback) = %q, want %q", got, input)
	}
}

// ---- PrototypeManager.Generate ----

func TestPrototypeManager_Generate_Success(t *testing.T) {
	pm, storage := newTestPrototypeManager(t, sampleHTML)
	ctx := context.Background()

	p, err := pm.Generate(ctx, 42, "建立定價頁面")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if p.ID == "" {
		t.Error("Generate: ID is empty")
	}
	if p.ChatID != 42 {
		t.Errorf("Generate: ChatID = %d, want 42", p.ChatID)
	}
	if p.OriginalPrompt != "建立定價頁面" {
		t.Errorf("Generate: OriginalPrompt = %q", p.OriginalPrompt)
	}
	if p.CurrentHTML != sampleHTML {
		t.Errorf("Generate: CurrentHTML mismatch")
	}
	if p.Version != 1 {
		t.Errorf("Generate: Version = %d, want 1", p.Version)
	}

	// verify persisted
	stored, err := storage.GetPrototype(p.ID)
	if err != nil {
		t.Fatalf("GetPrototype after Generate: %v", err)
	}
	if stored.OriginalPrompt != "建立定價頁面" {
		t.Errorf("stored OriginalPrompt = %q", stored.OriginalPrompt)
	}
}

func TestPrototypeManager_Generate_EmptyResponse(t *testing.T) {
	dir := t.TempDir()
	storage := openTestStorage(t)
	client := &protoMockClient{response: ""}
	pm, _ := NewPrototypeManager(client, dir, storage)

	_, err := pm.Generate(context.Background(), 1, "test")
	if err == nil || !strings.Contains(err.Error(), "no HTML found") {
		t.Errorf("Generate: expected 'no HTML found' error, got %v", err)
	}
}

// ---- PrototypeManager.Edit ----

func TestPrototypeManager_Edit_AppendsHistory(t *testing.T) {
	pm, storage := newTestPrototypeManager(t, sampleHTML)
	ctx := context.Background()

	original, _ := pm.Generate(ctx, 1, "初始原型")

	// Change mock response for edit
	pm.client.(*protoMockClient).response = `<!DOCTYPE html><html><body><h1>Edited</h1></body></html>`
	edited, err := pm.Edit(ctx, original.ID, "把標題改成 Edited")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if edited.Version != 2 {
		t.Errorf("Edit: Version = %d, want 2", edited.Version)
	}
	if len(edited.EditHistory) != 1 {
		t.Errorf("Edit: EditHistory len = %d, want 1", len(edited.EditHistory))
	}
	if edited.EditHistory[0].Prompt != "把標題改成 Edited" {
		t.Errorf("Edit: EditHistory[0].Prompt = %q", edited.EditHistory[0].Prompt)
	}

	// verify persisted
	stored, _ := storage.GetPrototype(original.ID)
	if stored.Version != 2 {
		t.Errorf("stored Version = %d, want 2", stored.Version)
	}
}

func TestPrototypeManager_Edit_MultipleEdits(t *testing.T) {
	pm, _ := newTestPrototypeManager(t, sampleHTML)
	ctx := context.Background()

	p, _ := pm.Generate(ctx, 1, "原型")
	pm.client.(*protoMockClient).response = sampleHTML

	for i := 0; i < 3; i++ {
		p2, err := pm.Edit(ctx, p.ID, "修改")
		if err != nil {
			t.Fatalf("Edit %d: %v", i, err)
		}
		p = p2
	}

	if len(p.EditHistory) != 3 {
		t.Errorf("after 3 edits, EditHistory len = %d, want 3", len(p.EditHistory))
	}
	if p.Version != 4 {
		t.Errorf("after 3 edits, Version = %d, want 4", p.Version)
	}
}

// ---- PrototypeManager.List ----

func TestPrototypeManager_List_FiltersByChatID(t *testing.T) {
	pm, _ := newTestPrototypeManager(t, sampleHTML)
	ctx := context.Background()

	pm.Generate(ctx, 10, "chat10 proto1")
	pm.Generate(ctx, 10, "chat10 proto2")
	pm.Generate(ctx, 99, "chat99 proto")

	list10, err := pm.List(ctx, 10)
	if err != nil {
		t.Fatalf("List(10): %v", err)
	}
	if len(list10) != 2 {
		t.Errorf("List(10): got %d items, want 2", len(list10))
	}

	list99, err := pm.List(ctx, 99)
	if err != nil {
		t.Fatalf("List(99): %v", err)
	}
	if len(list99) != 1 {
		t.Errorf("List(99): got %d items, want 1", len(list99))
	}

	listEmpty, err := pm.List(ctx, 999)
	if err != nil {
		t.Fatalf("List(999): %v", err)
	}
	if len(listEmpty) != 0 {
		t.Errorf("List(999): got %d items, want 0", len(listEmpty))
	}
}

// ---- PrototypeManager.Export ----

func TestPrototypeManager_Export_WritesFile(t *testing.T) {
	pm, _ := newTestPrototypeManager(t, sampleHTML)
	ctx := context.Background()

	p, _ := pm.Generate(ctx, 1, "匯出測試")
	path, err := pm.Export(ctx, p.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if !strings.HasSuffix(path, ".html") {
		t.Errorf("Export: path %q does not end with .html", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile exported: %v", err)
	}
	if string(content) != sampleHTML {
		t.Errorf("Export: file content mismatch")
	}
}

func TestPrototypeManager_Export_ContainsIDAndVersion(t *testing.T) {
	pm, _ := newTestPrototypeManager(t, sampleHTML)
	ctx := context.Background()

	p, _ := pm.Generate(ctx, 1, "版本測試")
	path, _ := pm.Export(ctx, p.ID)

	base := filepath.Base(path)
	if !strings.Contains(base, p.ID[:8]) {
		t.Errorf("export filename %q does not contain ID prefix %s", base, p.ID[:8])
	}
	if !strings.Contains(base, "v1") {
		t.Errorf("export filename %q does not contain version", base)
	}
}

func TestShortPrototypeIDAcceptsShortIDs(t *testing.T) {
	if got := shortPrototypeID("abc"); got != "abc" {
		t.Fatalf("shortPrototypeID(short) = %q, want abc", got)
	}
	if got := shortPrototypeID("123456789"); got != "12345678" {
		t.Fatalf("shortPrototypeID(long) = %q, want 12345678", got)
	}
}

// ---- SQLiteStorage prototype CRUD ----

func TestSQLiteStorage_Prototype_CRUD(t *testing.T) {
	s := openTestStorage(t)

	p := Prototype{
		ID:             "test-id-1",
		ChatID:         42,
		OriginalPrompt: "測試 CRUD",
		CurrentHTML:    sampleHTML,
		EditHistory:    []EditRecord{},
		Version:        1,
		MessageID:      100,
	}

	// Create
	if err := s.CreatePrototype(p); err != nil {
		t.Fatalf("CreatePrototype: %v", err)
	}

	// Get
	got, err := s.GetPrototype("test-id-1")
	if err != nil {
		t.Fatalf("GetPrototype: %v", err)
	}
	if got.OriginalPrompt != p.OriginalPrompt {
		t.Errorf("GetPrototype: OriginalPrompt = %q, want %q", got.OriginalPrompt, p.OriginalPrompt)
	}
	if got.CurrentHTML != sampleHTML {
		t.Errorf("GetPrototype: CurrentHTML mismatch")
	}

	// Update
	got.CurrentHTML = `<!DOCTYPE html><html><body>Updated</body></html>`
	got.Version = 2
	got.EditHistory = []EditRecord{{Prompt: "update1"}}
	if err := s.UpdatePrototype(got); err != nil {
		t.Fatalf("UpdatePrototype: %v", err)
	}

	got2, _ := s.GetPrototype("test-id-1")
	if got2.Version != 2 {
		t.Errorf("after Update: Version = %d, want 2", got2.Version)
	}
	if len(got2.EditHistory) != 1 {
		t.Errorf("after Update: EditHistory len = %d, want 1", len(got2.EditHistory))
	}
}

func TestSQLiteStorage_ListPrototypesByChat(t *testing.T) {
	s := openTestStorage(t)

	for i, chatID := range []int64{1, 1, 2} {
		p := Prototype{
			ID:             "id-" + string(rune('a'+i)),
			ChatID:         chatID,
			OriginalPrompt: "prompt",
			CurrentHTML:    sampleHTML,
			EditHistory:    []EditRecord{},
			Version:        1,
		}
		if err := s.CreatePrototype(p); err != nil {
			t.Fatalf("CreatePrototype: %v", err)
		}
	}

	list1, err := s.ListPrototypesByChat(1)
	if err != nil {
		t.Fatalf("ListPrototypesByChat(1): %v", err)
	}
	if len(list1) != 2 {
		t.Errorf("chat 1: got %d, want 2", len(list1))
	}

	list2, err := s.ListPrototypesByChat(2)
	if err != nil {
		t.Fatalf("ListPrototypesByChat(2): %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("chat 2: got %d, want 1", len(list2))
	}
}

func TestSQLiteStorage_GetPrototypeByMessageID(t *testing.T) {
	s := openTestStorage(t)

	p := Prototype{
		ID:             "msg-proto-1",
		ChatID:         5,
		OriginalPrompt: "msg test",
		CurrentHTML:    sampleHTML,
		EditHistory:    []EditRecord{},
		Version:        1,
		MessageID:      9999,
	}
	if err := s.CreatePrototype(p); err != nil {
		t.Fatalf("CreatePrototype: %v", err)
	}

	got, err := s.GetPrototypeByMessageID(9999)
	if err != nil {
		t.Fatalf("GetPrototypeByMessageID: %v", err)
	}
	if got.ID != "msg-proto-1" {
		t.Errorf("GetPrototypeByMessageID: ID = %q, want msg-proto-1", got.ID)
	}
}
