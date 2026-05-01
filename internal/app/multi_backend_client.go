package app

import (
	"context"
	"fmt"
	"strings"
)

// MultiBackendClient routes calls to different backend clients based on model name.
// It implements the Client interface and dispatches to claude/codex/api backends.
type MultiBackendClient struct {
	claude         *CLIClient   // Claude CLI backend
	codex          *CodexClient // Codex/OpenAI backend
	api            *APIClient   // Anthropic API backend
	defaultBackend Client       // Fallback backend when model doesn't match any prefix
}

// NewMultiBackendClient creates a dispatcher with all available backends.
// At least defaultBackend must be provided (typically a CLIClient or APIClient).
func NewMultiBackendClient(claude *CLIClient, codex *CodexClient, api *APIClient, defaultBackend Client) *MultiBackendClient {
	return &MultiBackendClient{
		claude:         claude,
		codex:          codex,
		api:            api,
		defaultBackend: defaultBackend,
	}
}

// HasCodex reports whether the codex backend is configured.
// Used by command handlers (/gfast /gsmart /gdeep, /model gpt-*) to reject early.
func (m *MultiBackendClient) HasCodex() bool { return m.codex != nil }

// routeFor returns the appropriate backend for a given model name, or an error
// if the model maps to a backend that wasn't configured (e.g. gpt-* with no codex).
// Priority:
//   - claude* -> CLIClient
//   - gpt-*, o3*, o4*, *codex -> CodexClient
//   - otherwise -> defaultBackend
func (m *MultiBackendClient) routeFor(model string) (Client, error) {
	lowerModel := strings.ToLower(model)

	if strings.HasPrefix(lowerModel, "claude") {
		if m.claude == nil {
			return nil, fmt.Errorf("model %q requires Claude CLI backend, but it is not configured", model)
		}
		return m.claude, nil
	}

	if strings.HasPrefix(lowerModel, "gpt-") ||
		strings.HasPrefix(lowerModel, "o3") ||
		strings.HasPrefix(lowerModel, "o4") ||
		strings.Contains(lowerModel, "codex") {
		if m.codex == nil {
			return nil, fmt.Errorf("model %q requires Codex backend (set OPENAI_API_KEY or multimedia.openai_api_key)", model)
		}
		return m.codex, nil
	}

	if m.defaultBackend == nil {
		return nil, fmt.Errorf("no backend configured for model %q", model)
	}
	return m.defaultBackend, nil
}

// Call invokes the appropriate backend based on modelOverride.
// If modelOverride is empty, uses defaultBackend.
func (m *MultiBackendClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	if modelOverride == "" {
		return m.defaultBackend.Call(ctx, message, projectDir, sessionID, "")
	}
	backend, err := m.routeFor(modelOverride)
	if err != nil {
		return nil, err
	}
	return backend.Call(ctx, message, projectDir, sessionID, modelOverride)
}

// CallStream invokes the appropriate backend based on modelOverride.
// If modelOverride is empty, uses defaultBackend.
func (m *MultiBackendClient) CallStream(
	ctx context.Context,
	message, projectDir, sessionID, modelOverride string,
	onToolUse func(toolName string, toolInput map[string]interface{}),
	onContent func(contentType, text string),
) (*CLIResponse, error) {
	if modelOverride == "" {
		return m.defaultBackend.CallStream(ctx, message, projectDir, sessionID, "", onToolUse, onContent)
	}
	backend, err := m.routeFor(modelOverride)
	if err != nil {
		return nil, err
	}
	return backend.CallStream(ctx, message, projectDir, sessionID, modelOverride, onToolUse, onContent)
}

// CallPlan invokes the appropriate backend based on modelOverride.
// If modelOverride is empty, uses defaultBackend.
func (m *MultiBackendClient) CallPlan(
	ctx context.Context,
	message, projectDir, sessionID, modelOverride string,
	onContent func(contentType, text string),
) (*CLIResponse, error) {
	if modelOverride == "" {
		return m.defaultBackend.CallPlan(ctx, message, projectDir, sessionID, "", onContent)
	}
	backend, err := m.routeFor(modelOverride)
	if err != nil {
		return nil, err
	}
	return backend.CallPlan(ctx, message, projectDir, sessionID, modelOverride, onContent)
}

// GetModel returns the model of the default backend.
func (m *MultiBackendClient) GetModel() string {
	return m.defaultBackend.GetModel()
}
