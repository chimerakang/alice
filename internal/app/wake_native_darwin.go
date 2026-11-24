//go:build darwin && cgo

package app

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static int aliceSaverSessionActive(void) {
    CFDictionaryRef session = CGSessionCopyCurrentDictionary();
    if (session == NULL) return -1;
    CFTypeRef value = CFDictionaryGetValue(session, CFSTR("CGSSessionScreenIsLocked"));
    int active = value != NULL && CFEqual(value, kCFBooleanTrue);
    CFRelease(session);
    // This indicates the screen-saver/session UI, not whether a password is required.
    return active;
}
static int alicePostEscape(void) {
    if (!CGPreflightPostEventAccess()) return 1;
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, 53, true);
    CGEventRef up = CGEventCreateKeyboardEvent(NULL, 53, false);
    if (down == NULL || up == NULL) {
        if (down != NULL) CFRelease(down);
        if (up != NULL) CFRelease(up);
        return 2;
    }
    CGEventSetFlags(down, 0);
    CGEventSetFlags(up, 0);
    CGEventPost(kCGHIDEventTap, down);
    CGEventPost(kCGHIDEventTap, up);
    CFRelease(down);
    CFRelease(up);
    return 0;
}
*/
import "C"

import (
	"context"
	"fmt"
	"time"
)

func dismissScreenSaver(ctx context.Context) error {
	state := int(C.aliceSaverSessionActive())
	if state < 0 {
		return fmt.Errorf("screen saver session state unavailable")
	}
	if state == 0 {
		return nil
	} // Never send Escape to the user's normal application.
	switch int(C.alicePostEscape()) {
	case 1:
		return errWakeInputPermission
	case 2:
		return fmt.Errorf("could not create fixed Escape event")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errWakeNotDismissed
		case <-ticker.C:
			state = int(C.aliceSaverSessionActive())
			if state == 0 {
				return nil
			}
			if state < 0 {
				return fmt.Errorf("screen saver session state unavailable after Escape")
			}
		}
	}
}
