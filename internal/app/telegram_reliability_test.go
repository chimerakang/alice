package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type telegramTestTransport func(*http.Request) (*http.Response, error)

func (f telegramTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func telegramTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestTelegramPollingDeadlineIncludesHeadersAndBody(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		t.Run(fmt.Sprint(bodyStarted), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if bodyStarted {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			bot := &TelegramBot{pollHTTPClient: &http.Client{Timeout: 40 * time.Millisecond}}
			var result interface{}
			started := time.Now()
			err := bot.pollUpdates(context.Background(), server.URL, &result)
			if err == nil {
				t.Fatal("stalled poll reported success")
			}
			if time.Since(started) > time.Second {
				t.Fatal("poll did not respect deadline")
			}
			if state, _ := bot.pollHealth.snapshot(time.Now()); state != "degraded" {
				t.Fatalf("health=%s", state)
			}
		})
	}
}
func TestPollingRecoveryAndHealth(t *testing.T) {
	calls := 0
	bot := &TelegramBot{pollHTTPClient: &http.Client{Transport: telegramTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return telegramTestResponse(429, `{"ok":false,"error_code":429,"parameters":{"retry_after":7}}`), nil
		}
		return telegramTestResponse(200, `{"ok":true,"result":[]}`), nil
	})}}
	var result struct {
		OK bool `json:"ok"`
	}
	if state, _ := bot.pollHealth.snapshot(time.Now()); state != "starting" {
		t.Fatal(state)
	}
	err := bot.pollUpdates(context.Background(), "https://example.invalid/botSECRET/getUpdates", &result)
	if telegramRetryDelay(err, 1) != 7*time.Second {
		t.Fatal("retry_after not respected")
	}
	if err = bot.pollUpdates(context.Background(), "https://example.invalid/getUpdates", &result); err != nil || !result.OK {
		t.Fatalf("recovery: %v", err)
	}
	if state, _ := bot.pollHealth.snapshot(time.Now()); state != "active" {
		t.Fatal(state)
	}
	if state, _ := bot.pollHealth.snapshot(time.Now().Add(telegramPollStaleAfter + time.Second)); state != "stale" {
		t.Fatal(state)
	}
	bot.Stop()
	if state, _ := bot.pollHealth.snapshot(time.Now()); state != "stopped" {
		t.Fatal(state)
	}
}
func TestTelegramRetryDelayAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want time.Duration
	}{{1, time.Second}, {2, 2 * time.Second}, {20, 30 * time.Second}} {
		if got := telegramRetryDelay(errors.New("offline"), tc.n); got != tc.want {
			t.Fatalf("%d: %v", tc.n, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(waitTelegramRetry(ctx, time.Hour), context.Canceled) {
		t.Fatal("retry did not cancel")
	}
}
func TestTelegramTransportErrorsDoNotExposeToken(t *testing.T) {
	bot := &TelegramBot{apiHTTPClient: &http.Client{Transport: telegramTestTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}}
	_, err := bot.telegramRequest(context.Background(), http.MethodGet, "https://example.invalid/botTOPSECRET/getMe", nil, false)
	if err == nil || strings.Contains(err.Error(), "TOPSECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
}
func TestSyncSendRetriesAreBoundedAndCloseBodies(t *testing.T) {
	calls := 0
	closed := 0
	bot := &TelegramBot{config: &Config{TelegramToken: "test"}, rateLimiter: time.NewTicker(time.Millisecond)}
	defer bot.rateLimiter.Stop()
	bot.apiHTTPClient = &http.Client{Transport: telegramTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Body: &telegramTrackedBody{Reader: strings.NewReader(`{"ok":false,"parameters":{"retry_after":0}}`), closed: &closed}, Header: make(http.Header)}, nil
	})}
	_, err := bot.sendMessageSync("sendMessage", map[string]interface{}{"text": "test"})
	if err == nil || calls != telegramMaxSendAttempts || closed != calls {
		t.Fatalf("calls=%d closed=%d err=%v", calls, closed, err)
	}
}

type telegramTrackedBody struct {
	io.Reader
	closed *int
}

func (b *telegramTrackedBody) Close() error { *b.closed++; return nil }
func TestStopCancelsInflightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	bot := &TelegramBot{queueCtx: ctx, queueCancel: cancel, apiHTTPClient: &http.Client{Transport: telegramTestTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	go func() {
		_, err := bot.telegramRequest(bot.telegramContext(), http.MethodPost, "https://example.invalid/sendMessage", strings.NewReader(`{}`), false)
		done <- err
	}()
	<-started
	bot.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel request")
	}
}
func TestHealthEndpointReflectsPolling(t *testing.T) {
	bot := &TelegramBot{}
	web := &WebInterface{bot: bot}
	for _, tc := range []struct {
		err  error
		want int
	}{{errors.New("offline"), 503}, {nil, 200}} {
		bot.pollHealth.record(tc.err)
		w := httptest.NewRecorder()
		web.handleHealth(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if w.Code != tc.want {
			t.Fatalf("HTTP %d; want %d", w.Code, tc.want)
		}
	}
}
func TestSyncRetryWaitCancelsOnStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	entered := make(chan struct{})
	bot := &TelegramBot{config: &Config{}, queueCtx: ctx, queueCancel: cancel, rateLimiter: time.NewTicker(time.Millisecond)}
	defer bot.rateLimiter.Stop()
	bot.apiHTTPClient = &http.Client{Transport: telegramTestTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		return telegramTestResponse(429, `{"ok":false,"parameters":{"retry_after":3600}}`), nil
	})}
	done := make(chan error, 1)
	go func() { _, err := bot.sendMessageSync("sendMessage", nil); done <- err }()
	<-entered
	bot.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry_after blocked shutdown")
	}
	if calls.Load() != 1 {
		t.Fatal("retried after cancellation")
	}
}
