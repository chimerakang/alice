package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type captureMediaTransport struct {
	path        string
	contentType string
	body        []byte
}

func (t *captureMediaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.path = req.URL.Path
	t.contentType = req.Header.Get("Content-Type")
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	t.body = body

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true,"result":true}`)),
		Request:    req,
	}, nil
}

func newTelegramMediaTestBot(chatID int64, lang string, maxFileSizeMB int) *TelegramBot {
	bot := &TelegramBot{
		config: &Config{
			TelegramToken: "bot-token",
			Multimedia: MultimediaConfig{
				MaxFileSizeMB: maxFileSizeMB,
			},
		},
		agents:          map[chatKey]*Agent{},
		chatContexts:    map[chatKey]*ChatContext{},
		messageQueue:    make(chan *TelegramMessage, 4),
		langPreferences: map[int64]string{},
	}
	if lang != "" {
		bot.langPreferences[chatID] = lang
	}
	return bot
}

func TestHandleSendFileRoutesSupportedTypesToTelegramAPI(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	cases := []struct {
		name          string
		fileName      string
		wantPathFrag  string
		wantFieldName string
	}{
		{name: "photo", fileName: "sample.png", wantPathFrag: "/botbot-token/sendPhoto", wantFieldName: "photo"},
		{name: "video", fileName: "sample.mov", wantPathFrag: "/botbot-token/sendVideo", wantFieldName: "video"},
		{name: "document", fileName: "sample.csv", wantPathFrag: "/botbot-token/sendDocument", wantFieldName: "document"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := &captureMediaTransport{}
			bot := newTelegramMediaTestBot(42, "zh-TW", 5)
			bot.i18n = i18n
			bot.mediaHTTPClient = &http.Client{Transport: transport}

			dir := t.TempDir()
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd: %v", err)
			}
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("Chdir: %v", err)
			}
			defer func() {
				_ = os.Chdir(cwd)
			}()

			filePath := filepath.Join("temp", tc.fileName)
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(filePath, []byte("payload"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			bot.handleSendFile(chatKey{chatID: 42}, filePath)

			if transport.path != tc.wantPathFrag {
				t.Fatalf("request path = %q, want %q", transport.path, tc.wantPathFrag)
			}
			if !strings.Contains(string(transport.body), `name="`+tc.wantFieldName+`"`) {
				t.Fatalf("multipart body missing %q field:\n%s", tc.wantFieldName, string(transport.body))
			}
			assertQueuedMessageContains(t, bot.messageQueue, "檔案已成功發送")
		})
	}
}

func TestHandleSendFileReportsTooLargeAndSkipsTelegramCall(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	bot := newTelegramMediaTestBot(42, "zh-TW", 1)
	bot.i18n = i18n
	bot.mediaHTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("Telegram API should not be called for oversized files")
		return nil, nil
	})}

	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	filePath := filepath.Join("temp", "oversized.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filePath, bytes.Repeat([]byte("a"), 2*1024*1024), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	bot.handleSendFile(chatKey{chatID: 42}, filePath)

	assertQueuedMessageContains(t, bot.messageQueue, "檔案太大，不浪費流量")
}

func TestHandleSendFileReportsUnsupportedDirectory(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	bot := newTelegramMediaTestBot(42, "zh-TW", 5)
	bot.i18n = i18n
	bot.mediaHTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("Telegram API should not be called for directories")
		return nil, nil
	})}

	baseDir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(baseDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	dir := filepath.Join("temp", "nested")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	bot.handleSendFile(chatKey{chatID: 42}, dir)

	assertQueuedMessageContains(t, bot.messageQueue, "不支援此檔案類型")
}

func TestProcessAgentResponseSendsMarkedFile(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	transport := &captureMediaTransport{}
	bot := newTelegramMediaTestBot(42, "zh-TW", 5)
	bot.i18n = i18n
	bot.mediaHTTPClient = &http.Client{Transport: transport}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "temp", "auto.png")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// processAgentResponse only accepts relative paths that stay inside allowed roots.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	got := bot.processAgentResponse(chatKey{chatID: 42}, "先看圖\n\n[SEND_FILE:temp/auto.png]", dir)

	if got != "先看圖" {
		t.Fatalf("processed response = %q, want %q", got, "先看圖")
	}
	if transport.path != "/botbot-token/sendPhoto" {
		t.Fatalf("request path = %q, want /botbot-token/sendPhoto", transport.path)
	}
}

func TestIsPathAllowedAcceptsRelativeTempFiles(t *testing.T) {
	if !isPathAllowed("temp/sample.png", "") {
		t.Fatal("temp/sample.png should be allowed")
	}
	if isPathAllowed("/tmp/sample.png", "") {
		t.Fatal("absolute path should not be allowed")
	}
}

func TestIsPathAllowedAcceptsProjectRelativeFiles(t *testing.T) {
	projectDir := t.TempDir()
	if !isPathAllowed("docs/report.pdf", projectDir) {
		t.Fatal("docs/report.pdf should be allowed inside the project root")
	}
	if isPathAllowed("../escape.png", projectDir) {
		t.Fatal("path traversal should not be allowed")
	}
}

func TestScanAndSendRecentMediaFilesSendsOnlyFreshSupportedFilesOnce(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	transport := &recordingMediaTransport{}
	bot := newTelegramMediaTestBot(42, "zh-TW", 1)
	bot.i18n = i18n
	bot.mediaHTTPClient = &http.Client{Transport: transport}

	projectDir := t.TempDir()
	now := time.Now()
	since := now.Add(-10 * time.Minute)

	oldFile := filepath.Join(projectDir, "temp", "old.png")
	recentPhoto := filepath.Join(projectDir, "temp", "fresh.png")
	recentDoc := filepath.Join(projectDir, "docs", "fresh.txt")
	oversized := filepath.Join(projectDir, "temp", "huge.png")

	for _, path := range []string{oldFile, recentPhoto, recentDoc, oversized} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", path, err)
		}
	}
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile(old): %v", err)
	}
	if err := os.WriteFile(recentPhoto, []byte("photo"), 0o644); err != nil {
		t.Fatalf("WriteFile(photo): %v", err)
	}
	if err := os.WriteFile(recentDoc, []byte("doc"), 0o644); err != nil {
		t.Fatalf("WriteFile(doc): %v", err)
	}
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), 2*1024*1024), 0o644); err != nil {
		t.Fatalf("WriteFile(oversized): %v", err)
	}

	oldStamp := now.Add(-time.Hour)
	recentStamp := now.Add(-time.Minute)
	if err := os.Chtimes(oldFile, oldStamp, oldStamp); err != nil {
		t.Fatalf("Chtimes(old): %v", err)
	}
	if err := os.Chtimes(recentPhoto, recentStamp, recentStamp); err != nil {
		t.Fatalf("Chtimes(photo): %v", err)
	}
	if err := os.Chtimes(recentDoc, recentStamp, recentStamp); err != nil {
		t.Fatalf("Chtimes(doc): %v", err)
	}
	if err := os.Chtimes(oversized, recentStamp, recentStamp); err != nil {
		t.Fatalf("Chtimes(oversized): %v", err)
	}

	bot.scanAndSendRecentMediaFiles(chatKey{chatID: 42}, projectDir, since)

	if got := len(transport.paths); got != 2 {
		t.Fatalf("first scan sent %d media files, want 2\npaths=%v", got, transport.paths)
	}
	if transport.paths[0] != "/botbot-token/sendDocument" {
		t.Fatalf("first sent path = %q, want sendDocument", transport.paths[0])
	}
	if transport.paths[1] != "/botbot-token/sendPhoto" {
		t.Fatalf("second sent path = %q, want sendPhoto", transport.paths[1])
	}

	bot.scanAndSendRecentMediaFiles(chatKey{chatID: 42}, projectDir, since)
	if got := len(transport.paths); got != 2 {
		t.Fatalf("second scan should not duplicate files, got %d sends", got)
	}
}

type autoMediaFixtureClient struct {
	response   string
	createFile func(projectDir string) error
}

func (c *autoMediaFixtureClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *autoMediaFixtureClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	if c.createFile != nil {
		if err := c.createFile(projectDir); err != nil {
			return nil, err
		}
	}
	result := c.response
	if result == "" {
		result = "ok"
	}
	return &CLIResponse{
		Result:      result,
		TextContent: result,
		SessionID:   "stream-session",
	}, nil
}

func (c *autoMediaFixtureClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	if c.createFile != nil {
		if err := c.createFile(projectDir); err != nil {
			return nil, err
		}
	}
	result := c.response
	if result == "" {
		result = "plan"
	}
	return &CLIResponse{
		Result:      result,
		TextContent: result,
		SessionID:   "plan-session",
	}, nil
}

func (c *autoMediaFixtureClient) GetModel() string {
	return "sonnet"
}

func TestHandleMessageAutoScansRecentMediaAfterAgentRun(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	projectDir := t.TempDir()
	transport := &recordingMediaTransport{}
	bot := newTelegramMediaTestBot(42, "zh-TW", 5)
	bot.i18n = i18n
	bot.client = &autoMediaFixtureClient{
		response: "分析完成",
		createFile: func(projectDir string) error {
			path := filepath.Join(projectDir, "temp", "generated.png")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("generated"), 0o644)
		},
	}
	bot.config.DefaultProjectDir = projectDir
	bot.mediaHTTPClient = &http.Client{Transport: transport}

	key := chatKey{chatID: 42}
	bot.handleMessage(key, 1001, "請執行工具後回傳產物", "", nil, nil, nil, "", 1, 0)

	if got := len(transport.paths); got != 1 {
		t.Fatalf("media sends = %d, want 1", got)
	}
	if transport.paths[0] != "/botbot-token/sendPhoto" {
		t.Fatalf("media path = %q, want /botbot-token/sendPhoto", transport.paths[0])
	}
}

func TestRunAgentWithStopButtonModeAutoScansRecentMediaAfterPlanRun(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	projectDir := t.TempDir()
	transport := &recordingMediaTransport{}
	client := &autoMediaFixtureClient{
		response: "plan complete",
		createFile: func(projectDir string) error {
			path := filepath.Join(projectDir, "docs", "generated.txt")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("generated"), 0o644)
		},
	}
	bot := newTelegramMediaTestBot(42, "zh-TW", 5)
	bot.i18n = i18n
	bot.mediaHTTPClient = &http.Client{Transport: transport}

	agent := NewAgent(client, projectDir, 42, 0)
	agent.enablePlanMode = true

	got, err := bot.runAgentWithStopButtonMode(chatKey{chatID: 42}, agent, "分析文件並回傳輸出", "document")
	if err != nil {
		t.Fatalf("runAgentWithStopButtonMode: %v", err)
	}
	if got != "plan complete" {
		t.Fatalf("response = %q, want plan complete", got)
	}
	if gotSends := len(transport.paths); gotSends != 1 {
		t.Fatalf("media sends = %d, want 1", gotSends)
	}
	if transport.paths[0] != "/botbot-token/sendDocument" {
		t.Fatalf("media path = %q, want /botbot-token/sendDocument", transport.paths[0])
	}
}

func TestHelpMentionsSendFileCommand(t *testing.T) {
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}

	bot := newTelegramMediaTestBot(42, "zh-TW", 5)
	bot.i18n = i18n
	key := chatKey{chatID: 42}

	bot.handleCommand(key, "/help")

	assertQueuedMessageContains(t, bot.messageQueue, "/send-file <檔案路徑>")
}

func TestRegisterCommandsIncludesSendFile(t *testing.T) {
	transport := &captureMediaTransport{}
	bot := newTelegramMediaTestBot(42, "zh-TW", 5)
	bot.apiHTTPClient = &http.Client{Transport: transport}

	bot.registerCommands()

	if transport.path != "/botbot-token/setMyCommands" {
		t.Fatalf("request path = %q, want /botbot-token/setMyCommands", transport.path)
	}
	if !strings.Contains(string(transport.body), "send-file") {
		t.Fatalf("command payload missing send-file:\n%s", string(transport.body))
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type recordingMediaTransport struct {
	paths []string
}

func (t *recordingMediaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.paths = append(t.paths, req.URL.Path)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true,"result":true}`)),
		Request:    req,
	}, nil
}
