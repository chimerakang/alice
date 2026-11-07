package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestWakeAuthorizationAndResults(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		ids                  map[int64]bool
		user                 int64
		nonHuman             bool
		text, platform, want string
		fail                 bool
		calls                int
	}{
		{name: "empty allowlist", user: 42, text: "/wake", platform: "darwin", want: "wake_requested", calls: 1},
		{name: "unlisted", ids: map[int64]bool{43: true}, user: 42, text: "/wake", platform: "darwin", want: "wake_denied"},
		{name: "missing sender", ids: map[int64]bool{0: true}, text: "/wake", platform: "darwin", want: "wake_denied"},
		{name: "bot or anonymous", ids: map[int64]bool{42: true}, user: 42, nonHuman: true, text: "/wake", platform: "darwin", want: "wake_denied"},
		{name: "arguments", ids: map[int64]bool{42: true}, user: 42, text: "/wake ; touch /tmp/pwned", platform: "darwin", want: "wake_usage"},
		{name: "unsupported", ids: map[int64]bool{42: true}, user: 42, text: "/wake", platform: "linux", want: "wake_unsupported"},
		{name: "success", ids: map[int64]bool{42: true}, user: 42, text: "/wake", platform: "darwin", want: "wake_requested", calls: 1},
		{name: "mention", ids: map[int64]bool{42: true}, user: 42, text: " /wake@AliceBot\n", platform: "darwin", want: "wake_requested", calls: 1},
		{name: "failure", ids: map[int64]bool{42: true}, user: 42, text: "/wake", platform: "darwin", want: "wake_failed", fail: true, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &TelegramBot{allowIDs: tc.ids}
			calls := 0
			got := bot.wakeResult(tc.user, tc.nonHuman, tc.text, tc.platform, func(ctx context.Context) error {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Error("missing deadline")
				}
				if tc.fail {
					return errors.New("test failure")
				}
				return nil
			})
			if got != tc.want || calls != tc.calls {
				t.Fatalf("result=%s calls=%d; want %s calls=%d", got, calls, tc.want, tc.calls)
			}
		})
	}
}

func TestDisplayWakeCommandIsFixed(t *testing.T) {
	cmd := displayWakeCommand(context.Background())
	if cmd.Path != "/usr/bin/caffeinate" || !reflect.DeepEqual(cmd.Args, []string{"/usr/bin/caffeinate", "-u", "-t", "1"}) {
		t.Fatalf("unexpected command: %v", cmd.Args)
	}
	for _, text := range []string{"", "/wakeful", "hello /wake"} {
		if isWakeCommand(text) {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestWakeReportsPermissionAndIncompleteDismissal(t *testing.T) {
	bot := &TelegramBot{}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errWakeInputPermission, "wake_input_permission_required"},
		{errWakeNotDismissed, "wake_not_dismissed"},
		{errWakeNativeUnavailable, "wake_failed"},
	} {
		got := bot.wakeResult(42, false, "/wake", "darwin", func(context.Context) error { return tc.err })
		if got != tc.want {
			t.Errorf("error %v: got %s, want %s", tc.err, got, tc.want)
		}
	}
}
