//go:build !darwin || !cgo

package app

import "context"

func dismissScreenSaver(context.Context) error { return errWakeNativeUnavailable }
