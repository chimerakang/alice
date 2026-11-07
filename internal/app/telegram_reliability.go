package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	telegramAPITimeout      = 20 * time.Second
	telegramPollTimeout     = 75 * time.Second // Longer than Telegram's 60-second long poll.
	telegramPollStaleAfter  = 3 * telegramPollTimeout
	telegramMaxSendAttempts = 3
)

var (
	defaultTelegramAPIClient  = &http.Client{Timeout: telegramAPITimeout}
	defaultTelegramPollClient = &http.Client{Timeout: telegramPollTimeout}
)

// Preserve error classification without logging the credential-bearing request URL.
type telegramRequestError struct{ cause error }

func (e *telegramRequestError) Error() string {
	if errors.Is(e.cause, context.Canceled) {
		return "Telegram request canceled"
	}
	var networkError net.Error
	if errors.As(e.cause, &networkError) && networkError.Timeout() {
		return "Telegram request timed out"
	}
	return fmt.Sprintf("Telegram transport failure (%T)", e.cause)
}
func (e *telegramRequestError) Unwrap() error { return e.cause }

type telegramAPIError struct {
	Code       int
	RetryAfter int
}

func (e *telegramAPIError) Error() string { return fmt.Sprintf("Telegram API error %d", e.Code) }

type telegramPollingHealth struct {
	mu          sync.RWMutex
	lastSuccess time.Time
	failed      bool
	stopped     bool
}

func (h *telegramPollingHealth) record(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failed = err != nil
	h.stopped = false
	if err == nil {
		h.lastSuccess = time.Now()
	}
}
func (h *telegramPollingHealth) stop() { h.mu.Lock(); h.stopped = true; h.mu.Unlock() }
func (h *telegramPollingHealth) snapshot(now time.Time) (string, time.Time) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	switch {
	case h.stopped:
		return "stopped", h.lastSuccess
	case h.failed:
		return "degraded", h.lastSuccess
	case h.lastSuccess.IsZero():
		return "starting", h.lastSuccess
	case now.Sub(h.lastSuccess) > telegramPollStaleAfter:
		return "stale", h.lastSuccess
	default:
		return "active", h.lastSuccess
	}
}
func (t *TelegramBot) telegramContext() context.Context {
	if t.queueCtx != nil {
		return t.queueCtx
	}
	return context.Background()
}
func (t *TelegramBot) telegramRequest(ctx context.Context, method, endpoint string, body io.Reader, polling bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, &telegramRequestError{err}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := t.apiHTTPClient
	if polling {
		client = t.pollHTTPClient
	}
	if client == nil {
		if polling {
			client = defaultTelegramPollClient
		} else {
			client = defaultTelegramAPIClient
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &telegramRequestError{err}
	}
	return resp, nil
}
func (t *TelegramBot) pollUpdates(ctx context.Context, endpoint string, target interface{}) (err error) {
	defer func() { t.pollHealth.record(err) }()
	resp, err := t.telegramRequest(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	const maxBody = 16 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return &telegramRequestError{err}
	}
	if len(data) > maxBody {
		return errors.New("Telegram update response too large")
	}
	var envelope struct {
		OK         bool `json:"ok"`
		ErrorCode  int  `json:"error_code"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("invalid Telegram update JSON: %w", err)
	}
	if resp.StatusCode != http.StatusOK || !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &telegramAPIError{Code: code, RetryAfter: envelope.Parameters.RetryAfter}
	}
	return json.Unmarshal(data, target)
}
func telegramRetryDelay(err error, failures int) time.Duration {
	var apiErr *telegramAPIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return time.Duration(apiErr.RetryAfter) * time.Second
	}
	if failures < 1 {
		failures = 1
	}
	if failures > 6 {
		failures = 6
	}
	delay := time.Second << uint(failures-1)
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}
func waitTelegramRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Stop cancels in-flight requests, polling and queue retries before storage closes.
func (t *TelegramBot) Stop() {
	if t.queueCancel != nil {
		t.queueCancel()
	}
	t.pollHealth.stop()
}
