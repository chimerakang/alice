package app

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var (
	errWakeInputPermission   = errors.New("Alice lacks macOS permission to post normal input events")
	errWakeNotDismissed      = errors.New("screen saver session UI remained active after Escape")
	errWakeNativeUnavailable = errors.New("native macOS input support requires a cgo build")
)

func isWakeCommand(text string) bool {
	parts := strings.Fields(text)
	return len(parts) > 0 && strings.SplitN(parts[0], "@", 2)[0] == "/wake"
}

// wakeResult follows the bot's existing allowlist policy.
// The runner is never called for rejected requests.
func (t *TelegramBot) wakeResult(userID int64, nonHuman bool, text, platform string, run func(context.Context) error) string {
	if userID <= 0 || nonHuman || !t.isAllowed(userID) {
		return "wake_denied"
	}
	if !isWakeCommand(text) || len(strings.Fields(text)) != 1 {
		return "wake_usage"
	}
	if platform != "darwin" {
		return "wake_unsupported"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		log.Printf("[wake] request failed for user %d: %v", userID, err)
		switch {
		case errors.Is(err, errWakeInputPermission):
			return "wake_input_permission_required"
		case errors.Is(err, errWakeNotDismissed):
			return "wake_not_dismissed"
		}
		return "wake_failed"
	}
	log.Printf("[wake] display wake completed; screen saver session inactive for user %d", userID)
	return "wake_requested"
}

func displayWakeCommand(ctx context.Context) *exec.Cmd {
	// Fixed system executable and arguments; never invoke a shell or accept input.
	return exec.CommandContext(ctx, "/usr/bin/caffeinate", "-u", "-t", "1")
}

func wakeDisplayAndStopScreenSaver(ctx context.Context) error {
	if err := displayWakeCommand(ctx).Run(); err != nil {
		return err
	}
	return dismissScreenSaver(ctx)
}

func (t *TelegramBot) handleWakeCommand(key chatKey, userID int64, nonHuman bool, text string) {
	result := t.wakeResult(userID, nonHuman, text, runtime.GOOS, wakeDisplayAndStopScreenSaver)
	t.send(key, t.getLocalizedMessage(key.chatID, result, nil))
}
