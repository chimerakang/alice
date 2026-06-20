package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	appengine "claude-tg-agent/internal/app/engine"
)

const codexSessionRuntimeEventType = "CodexSessionUpdate"

type CodexSessionUpdatePayload struct {
	SessionID         string                 `json:"session_id"`
	Event             string                 `json:"event"`
	Source            string                 `json:"source"`
	SessionPath       string                 `json:"session_path"`
	ProjectDir        string                 `json:"project_dir,omitempty"`
	ThreadID          string                 `json:"thread_id,omitempty"`
	EventType         string                 `json:"event_type,omitempty"`
	ItemID            string                 `json:"item_id,omitempty"`
	ItemType          string                 `json:"item_type,omitempty"`
	Message           string                 `json:"message,omitempty"`
	Command           string                 `json:"command,omitempty"`
	Status            string                 `json:"status,omitempty"`
	ExitCode          *int                   `json:"exit_code,omitempty"`
	TokensInput       int                    `json:"tokens_input,omitempty"`
	CachedInputTokens int                    `json:"cached_input_tokens,omitempty"`
	TokensOutput      int                    `json:"tokens_output,omitempty"`
	Timestamp         string                 `json:"timestamp,omitempty"`
	Raw               map[string]interface{} `json:"raw,omitempty"`
}

type CodexSessionWatcher struct {
	sessionsDir string
	watcher     *fsnotify.Watcher
	mu          sync.Mutex
	offsets     map[string]int64
	threads     map[string]string
}

func defaultCodexSessionsDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".codex", "sessions")
	}
	return ""
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '~' {
		return path
	}
	if len(path) > 1 && path[1] != '/' && path[1] != '\\' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if len(path) == 1 {
		return home
	}
	return filepath.Join(home, path[2:])
}

func StartCodexSessionWatcher(ctx context.Context, cfg CodexInterceptionConfig) (*CodexSessionWatcher, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	dir := strings.TrimSpace(cfg.SessionsDir)
	if dir == "" {
		dir = defaultCodexSessionsDir()
	}
	dir = expandHomePath(dir)
	if dir == "" {
		return nil, fmt.Errorf("codex sessions dir unavailable")
	}
	w, err := newCodexSessionWatcher(dir)
	if err != nil {
		return nil, err
	}
	if err := w.start(ctx); err != nil {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

func newCodexSessionWatcher(sessionsDir string) (*CodexSessionWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &CodexSessionWatcher{
		sessionsDir: filepath.Clean(sessionsDir),
		watcher:     fsw,
		offsets:     map[string]int64{},
		threads:     map[string]string{},
	}, nil
}

func (w *CodexSessionWatcher) Close() error {
	if w == nil || w.watcher == nil {
		return nil
	}
	return w.watcher.Close()
}

func (w *CodexSessionWatcher) start(ctx context.Context) error {
	if err := os.MkdirAll(w.sessionsDir, 0755); err != nil {
		return err
	}
	if err := w.watchExisting(); err != nil {
		return err
	}
	go w.run(ctx)
	log.Printf("[codex-watch] watching sessions dir: %s", w.sessionsDir)
	return nil
}

func (w *CodexSessionWatcher) watchExisting() error {
	return filepath.WalkDir(w.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return w.addWatch(path)
		}
		if isCodexSessionJSONL(path) {
			w.markExistingFile(path)
		}
		return nil
	})
}

func (w *CodexSessionWatcher) addWatch(path string) error {
	if err := w.watcher.Add(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

func (w *CodexSessionWatcher) markExistingFile(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.offsets[filepath.Clean(path)] = info.Size()
	w.mu.Unlock()
}

func (w *CodexSessionWatcher) run(ctx context.Context) {
	defer w.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleFSEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[codex-watch] fsnotify error: %v", err)
		}
	}
}

func (w *CodexSessionWatcher) handleFSEvent(event fsnotify.Event) {
	path := filepath.Clean(event.Name)
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			_ = w.addWatch(path)
			return
		}
	}
	if !isCodexSessionJSONL(path) {
		return
	}
	if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
		return
	}
	if err := w.processFile(context.Background(), path); err != nil {
		log.Printf("[codex-watch] process %s: %v", path, err)
	}
}

func isCodexSessionJSONL(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}

func (w *CodexSessionWatcher) processFile(ctx context.Context, path string) error {
	path = filepath.Clean(path)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w.mu.Lock()
	offset := w.offsets[path]
	w.mu.Unlock()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	// Stream complete lines with bufio.Reader.ReadBytes instead of
	// bufio.Scanner. Codex rollout files can contain single JSONL lines far
	// larger than Scanner's token cap (4MB), which made Scan() fail with
	// bufio.ErrTooLong. processFile then returned early WITHOUT advancing the
	// offset, so every subsequent fsnotify Write re-scanned from the same
	// position, failed identically, and spammed the log forever (alice.log
	// grew past 450MB). ReadBytes has no per-line size limit and lets us
	// advance the offset even past oversized lines.
	reader := bufio.NewReader(f)
	pos := offset
	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil {
			// EOF mid-line: a partial line is still being written. Do not
			// consume it — leave the offset before it so the next Write event
			// re-reads the line once it is complete.
			break
		}
		pos += int64(len(lineBytes))
		line := bytes.TrimRight(lineBytes, "\r\n")
		payload, ok := w.parseLine(path, line)
		if !ok {
			continue
		}
		recordCodexSessionUpdate(ctx, payload)
	}

	w.mu.Lock()
	w.offsets[path] = pos
	w.mu.Unlock()
	return nil
}

func (w *CodexSessionWatcher) parseLine(path string, line []byte) (CodexSessionUpdatePayload, bool) {
	payload, ok := parseCodexSessionJSONLLine(path, line)
	if !ok {
		return CodexSessionUpdatePayload{}, false
	}
	if payload.ThreadID != "" {
		w.mu.Lock()
		w.threads[filepath.Clean(path)] = payload.ThreadID
		w.mu.Unlock()
	}
	if payload.ThreadID == "" {
		w.mu.Lock()
		payload.ThreadID = w.threads[filepath.Clean(path)]
		w.mu.Unlock()
	}
	if payload.SessionID == "" {
		payload.SessionID = payload.ThreadID
	}
	if payload.SessionID == "" {
		payload.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return payload, true
}

func parseCodexSessionJSONLLine(path string, line []byte) (CodexSessionUpdatePayload, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal(line, &raw); err != nil {
		return CodexSessionUpdatePayload{}, false
	}
	var ev codexEvent
	if err := json.Unmarshal(line, &ev); err != nil || strings.TrimSpace(ev.Type) == "" {
		return CodexSessionUpdatePayload{}, false
	}
	payload := CodexSessionUpdatePayload{
		Event:       "session_update",
		Source:      "codex-vscode",
		SessionPath: filepath.Clean(path),
		EventType:   ev.Type,
		ThreadID:    strings.TrimSpace(ev.ThreadID),
		Timestamp:   time.Now().Format(time.RFC3339),
		Raw:         raw,
	}
	if ev.Item != nil {
		payload.ItemID = ev.Item.ID
		payload.ItemType = ev.Item.Type
		payload.Message = ev.Item.Text
		payload.Command = ev.Item.Command
		payload.Status = ev.Item.Status
		payload.ExitCode = ev.Item.ExitCode
	}
	if ev.Message != "" {
		payload.Message = ev.Message
	}
	if ev.Error != nil && ev.Error.Message != "" {
		payload.Message = ev.Error.Message
		payload.Status = "failed"
	}
	if ev.Usage != nil {
		payload.TokensInput = ev.Usage.InputTokens
		payload.CachedInputTokens = ev.Usage.CachedInputTokens
		payload.TokensOutput = ev.Usage.OutputTokens
	}
	if ts, ok := raw["timestamp"].(string); ok && strings.TrimSpace(ts) != "" {
		payload.Timestamp = strings.TrimSpace(ts)
	}
	return payload, true
}

func recordCodexSessionUpdate(ctx context.Context, payload CodexSessionUpdatePayload) {
	if payload.Event == "" {
		payload.Event = "session_update"
	}
	if payload.Source == "" {
		payload.Source = "codex-vscode"
	}
	// Keep persisted runtime events compact; Raw is for parser/debug callers.
	payload.Raw = nil
	recordRuntimeEvent(ctx, appengine.Event{
		Type:      codexSessionRuntimeEventType,
		Timestamp: parseRuntimeEventTimestamp(payload.Timestamp),
		Payload:   payload,
	})
	if globalWebSocketHub != nil {
		globalWebSocketHub.BroadcastEvent("codex_session_update", payload)
	}
}

func parseRuntimeEventTimestamp(value string) time.Time {
	if value == "" {
		return time.Now()
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Now()
}
