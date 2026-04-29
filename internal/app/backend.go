package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// BackendType identifies the type of execution backend
type BackendType string

const (
	BackendLocal  BackendType = "local"
	BackendDocker BackendType = "docker"
	BackendSSH    BackendType = "ssh"
)

// ExecOptions configures command execution
type ExecOptions struct {
	Timeout time.Duration
	Env     map[string]string
	WorkDir string
}

// BackendInfo describes a backend's metadata
type BackendInfo struct {
	Type      BackendType `json:"type"`
	Name      string      `json:"name"`
	Available bool        `json:"available"`
	Details   string      `json:"details,omitempty"` // e.g. docker image, ssh host
}

// ExecutionBackend abstracts command and file execution across environments
type ExecutionBackend interface {
	// Execute runs a command and returns combined output
	Execute(ctx context.Context, cmd string, opts ExecOptions) (output string, err error)

	// WriteFile writes content to a file path
	WriteFile(ctx context.Context, path, content string) error

	// ReadFile reads content from a file path
	ReadFile(ctx context.Context, path string) (content string, err error)

	// DeleteFile removes a file
	DeleteFile(ctx context.Context, path string) error

	// GetWorkDir returns the backend's working directory
	GetWorkDir(ctx context.Context) (string, error)

	// Ping checks backend availability
	Ping(ctx context.Context) error

	// Info returns backend metadata
	Info() BackendInfo
}

// --- Local Backend ---

// LocalBackend executes commands and file operations on the local machine
type LocalBackend struct {
	workDir string
}

// NewLocalBackend creates a LocalBackend with the given working directory
func NewLocalBackend(workDir string) *LocalBackend {
	return &LocalBackend{workDir: workDir}
}

func (b *LocalBackend) Execute(ctx context.Context, cmd string, opts ExecOptions) (string, error) {
	workDir := b.workDir
	if opts.WorkDir != "" {
		workDir = opts.WorkDir
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	env := []string(nil)
	if len(opts.Env) > 0 {
		env = os.Environ()
		for k, v := range opts.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	c, cancel := processCommand(ctx, ProcessOptions{
		Dir:     workDir,
		Env:     env,
		Timeout: timeout,
	}, "sh", "-c", cmd)
	defer cancel()

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		combined := stdout.String() + stderr.String()
		if combined == "" {
			combined = err.Error()
		}
		return combined, fmt.Errorf("command failed: %w", err)
	}

	return stdout.String(), nil
}

func (b *LocalBackend) WriteFile(ctx context.Context, path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func (b *LocalBackend) ReadFile(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (b *LocalBackend) DeleteFile(ctx context.Context, path string) error {
	return os.Remove(path)
}

func (b *LocalBackend) GetWorkDir(ctx context.Context) (string, error) {
	return b.workDir, nil
}

func (b *LocalBackend) Ping(ctx context.Context) error {
	// Local is always available
	return nil
}

func (b *LocalBackend) Info() BackendInfo {
	return BackendInfo{
		Type:      BackendLocal,
		Name:      "local",
		Available: true,
		Details:   fmt.Sprintf("workdir=%s", b.workDir),
	}
}

// --- Docker Backend (stub for Phase 2) ---

// DockerBackend executes commands inside a Docker container
type DockerBackend struct {
	image       string
	containerID string
	workDir     string
	network     string
	memoryLimit string
	cpuLimit    string
}

// NewDockerBackend creates a DockerBackend (Phase 2 implementation)
func NewDockerBackend(image, workDir, network, memoryLimit, cpuLimit string) *DockerBackend {
	return &DockerBackend{
		image:       image,
		workDir:     workDir,
		network:     network,
		memoryLimit: memoryLimit,
		cpuLimit:    cpuLimit,
	}
}

func (b *DockerBackend) Execute(ctx context.Context, cmd string, opts ExecOptions) (string, error) {
	return "", fmt.Errorf("docker backend not yet implemented (Phase 2)")
}

func (b *DockerBackend) WriteFile(ctx context.Context, path, content string) error {
	return fmt.Errorf("docker backend not yet implemented (Phase 2)")
}

func (b *DockerBackend) ReadFile(ctx context.Context, path string) (string, error) {
	return "", fmt.Errorf("docker backend not yet implemented (Phase 2)")
}

func (b *DockerBackend) DeleteFile(ctx context.Context, path string) error {
	return fmt.Errorf("docker backend not yet implemented (Phase 2)")
}

func (b *DockerBackend) GetWorkDir(ctx context.Context) (string, error) {
	return b.workDir, nil
}

func (b *DockerBackend) Ping(ctx context.Context) error {
	// Check if docker is available
	_, err := runProcessOutput(ctx, ProcessOptions{Timeout: 10 * time.Second}, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}
	return nil
}

func (b *DockerBackend) Info() BackendInfo {
	available := false
	if err := b.Ping(context.Background()); err == nil {
		available = true
	}
	return BackendInfo{
		Type:      BackendDocker,
		Name:      "docker",
		Available: available,
		Details:   fmt.Sprintf("image=%s, network=%s", b.image, b.network),
	}
}

// --- SSH Backend (stub for Phase 3) ---

// SSHBackend executes commands on a remote host via SSH
type SSHBackend struct {
	host    string
	user    string
	keyPath string
	workDir string
}

// NewSSHBackend creates an SSHBackend (Phase 3 implementation)
func NewSSHBackend(host, user, keyPath, workDir string) *SSHBackend {
	return &SSHBackend{
		host:    host,
		user:    user,
		keyPath: keyPath,
		workDir: workDir,
	}
}

func (b *SSHBackend) Execute(ctx context.Context, cmd string, opts ExecOptions) (string, error) {
	return "", fmt.Errorf("SSH backend not yet implemented (Phase 3)")
}

func (b *SSHBackend) WriteFile(ctx context.Context, path, content string) error {
	return fmt.Errorf("SSH backend not yet implemented (Phase 3)")
}

func (b *SSHBackend) ReadFile(ctx context.Context, path string) (string, error) {
	return "", fmt.Errorf("SSH backend not yet implemented (Phase 3)")
}

func (b *SSHBackend) DeleteFile(ctx context.Context, path string) error {
	return fmt.Errorf("SSH backend not yet implemented (Phase 3)")
}

func (b *SSHBackend) GetWorkDir(ctx context.Context) (string, error) {
	return b.workDir, nil
}

func (b *SSHBackend) Ping(ctx context.Context) error {
	// Try SSH connection test
	output, err := runProcessOutput(ctx, ProcessOptions{Timeout: 10 * time.Second}, "ssh", "-o", "ConnectTimeout=5", "-o", "BatchMode=yes",
		"-i", b.keyPath, fmt.Sprintf("%s@%s", b.user, b.host), "echo ok")
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	if len(output) == 0 {
		return fmt.Errorf("SSH connection returned empty response")
	}
	_ = io.Discard // suppress unused import if needed
	return nil
}

func (b *SSHBackend) Info() BackendInfo {
	available := false
	if err := b.Ping(context.Background()); err == nil {
		available = true
	}
	return BackendInfo{
		Type:      BackendSSH,
		Name:      fmt.Sprintf("ssh-%s", b.host),
		Available: available,
		Details:   fmt.Sprintf("%s@%s:%s", b.user, b.host, b.workDir),
	}
}

// --- Backend Manager ---

// BackendManager manages multiple execution backends
type BackendManager struct {
	backends    map[string]ExecutionBackend
	defaultName string
	mu          sync.RWMutex
}

// Global backend manager instance
var globalBackendManager *BackendManager

// NewBackendManager creates a new BackendManager
func NewBackendManager() *BackendManager {
	return &BackendManager{
		backends:    make(map[string]ExecutionBackend),
		defaultName: "local",
	}
}

// Register adds a named backend
func (m *BackendManager) Register(name string, backend ExecutionBackend) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.backends[name]; exists {
		return fmt.Errorf("backend %q already registered", name)
	}

	m.backends[name] = backend
	log.Printf("[backend] registered: %s (%s)", name, backend.Info().Type)
	return nil
}

// Get returns a named backend
func (m *BackendManager) Get(name string) (ExecutionBackend, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	b, ok := m.backends[name]
	if !ok {
		return nil, fmt.Errorf("backend %q not found", name)
	}
	return b, nil
}

// Default returns the default backend
func (m *BackendManager) Default() ExecutionBackend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if b, ok := m.backends[m.defaultName]; ok {
		return b
	}
	// Fallback to "local" if default name doesn't exist
	if b, ok := m.backends["local"]; ok {
		return b
	}
	return nil
}

// DefaultName returns the name of the current default backend
func (m *BackendManager) DefaultName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultName
}

// SetDefault changes the default backend
func (m *BackendManager) SetDefault(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.backends[name]; !ok {
		return fmt.Errorf("backend %q not found", name)
	}

	m.defaultName = name
	log.Printf("[backend] default changed to: %s", name)
	return nil
}

// ListAll returns info about all registered backends
func (m *BackendManager) ListAll() []BackendInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var infos []BackendInfo
	for _, b := range m.backends {
		infos = append(infos, b.Info())
	}
	return infos
}

// HealthCheckAll pings all backends and returns any errors
func (m *BackendManager) HealthCheckAll(ctx context.Context) map[string]error {
	m.mu.RLock()
	backends := make(map[string]ExecutionBackend, len(m.backends))
	for k, v := range m.backends {
		backends[k] = v
	}
	m.mu.RUnlock()

	results := make(map[string]error, len(backends))
	for name, b := range backends {
		results[name] = b.Ping(ctx)
	}
	return results
}

// InitBackendManager creates and initializes the global BackendManager
func InitBackendManager(defaultWorkDir string) {
	globalBackendManager = NewBackendManager()

	// Always register local backend
	local := NewLocalBackend(defaultWorkDir)
	globalBackendManager.Register("local", local)

	log.Printf("[backend] manager initialized (default=local, workdir=%s)", defaultWorkDir)
}
