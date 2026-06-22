package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// RuntimeLock keeps one bot process active for a given data directory.
type RuntimeLock struct {
	file *os.File
	path string
}

func AcquireRuntimeLock(lockPath string) (*RuntimeLock, error) {
	if lockPath == "" {
		return nil, fmt.Errorf("lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open runtime lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another alice bot process is already running for %s", lockPath)
	}
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, fmt.Errorf("truncate runtime lock: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		file.Close()
		return nil, fmt.Errorf("write runtime lock: %w", err)
	}
	return &RuntimeLock{file: file, path: lockPath}, nil
}

func (l *RuntimeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}

func runtimeLockPath(databasePath string) string {
	if databasePath == "" {
		databasePath = filepath.Join(".", "data", "alice.db")
	}
	return filepath.Join(filepath.Dir(databasePath), "alice.lock")
}
