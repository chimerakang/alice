package engine

import "testing"

func TestDecideSessionPolicyDirectModesUseRecentOnly(t *testing.T) {
	for _, mode := range []string{"direct_resume_fallback", "direct_model_switch", "direct_continuation"} {
		t.Run(mode, func(t *testing.T) {
			decision := DecideSessionPolicy(SessionPolicyRequest{Mode: mode, HasNativeSession: true})
			if decision.Policy != SessionPolicyMemoryOnly {
				t.Fatalf("Policy = %q, want %q", decision.Policy, SessionPolicyMemoryOnly)
			}
			if decision.UseNativeSession {
				t.Fatal("UseNativeSession = true, want false")
			}
			if !decision.AllowRecentMemory {
				t.Fatal("AllowRecentMemory = false, want true")
			}
			if decision.AllowGeneralMemory || decision.AllowStaticHints {
				t.Fatalf("broad memory allowed: general=%v static=%v", decision.AllowGeneralMemory, decision.AllowStaticHints)
			}
		})
	}
}

func TestDecideSessionPolicyDirectIssueScopeSkipsRecent(t *testing.T) {
	decision := DecideSessionPolicy(SessionPolicyRequest{
		Mode:          "direct_resume_fallback",
		HasIssueScope: true,
	})
	if decision.AllowRecentMemory {
		t.Fatal("AllowRecentMemory = true, want false for explicit issue scope")
	}
}

func TestDecideSessionPolicyExplicitIssueAllowsScopedSources(t *testing.T) {
	decision := DecideSessionPolicy(SessionPolicyRequest{
		Mode:             "hermes",
		HasNativeSession: true,
		HasIssueScope:    true,
	})
	if decision.Policy != SessionPolicyHybridIssueOnly {
		t.Fatalf("Policy = %q, want %q", decision.Policy, SessionPolicyHybridIssueOnly)
	}
	if !decision.UseNativeSession || !decision.AllowGeneralMemory || !decision.AllowStaticHints {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if decision.AllowRecentMemory {
		t.Fatal("AllowRecentMemory = true, want false for explicit issue scope")
	}
}

func TestDecideSessionRunPriorityAndSession(t *testing.T) {
	tests := []struct {
		name        string
		req         SessionRunRequest
		wantModel   string
		wantReason  string
		wantBackend BackendKind
		wantSession string
	}{
		{
			name: "override wins",
			req: SessionRunRequest{
				OverrideModel:   "gpt-5.5",
				StickyModel:     "claude-opus-4-6",
				RoutedModel:     "haiku",
				BackendSessions: map[BackendKind]string{BackendCodex: "codex-session"},
			},
			wantModel:   "gpt-5.5",
			wantReason:  "user_command",
			wantBackend: BackendCodex,
			wantSession: "codex-session",
		},
		{
			name: "sticky beats continuation and routed",
			req: SessionRunRequest{
				StickyModel:       "claude-opus-4-6",
				ContinuationModel: "gpt-5.5",
				RoutedModel:       "haiku",
				BackendSessions:   map[BackendKind]string{BackendClaude: "claude-session"},
			},
			wantModel:   "claude-opus-4-6",
			wantReason:  "sticky_session",
			wantBackend: BackendClaude,
			wantSession: "claude-session",
		},
		{
			name: "continuation beats routed",
			req: SessionRunRequest{
				ContinuationModel: "gpt-5.5",
				RoutedModel:       "haiku",
			},
			wantModel:   "gpt-5.5",
			wantReason:  "follow_up",
			wantBackend: BackendCodex,
		},
		{
			name: "routed model",
			req: SessionRunRequest{
				RoutedModel:  "haiku",
				RoutedReason: "static_rule",
			},
			wantModel:   "haiku",
			wantReason:  "static_rule",
			wantBackend: BackendClaude,
		},
		{
			name: "default model",
			req: SessionRunRequest{
				DefaultModel: "sonnet",
			},
			wantModel:   "sonnet",
			wantReason:  "default",
			wantBackend: BackendClaude,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideSessionRun(tt.req)
			if got.Model != tt.wantModel || got.RoutingReason != tt.wantReason {
				t.Fatalf("model decision = (%q, %q), want (%q, %q)", got.Model, got.RoutingReason, tt.wantModel, tt.wantReason)
			}
			if got.Backend != tt.wantBackend {
				t.Fatalf("Backend = %v, want %v", got.Backend, tt.wantBackend)
			}
			if got.SessionID != tt.wantSession {
				t.Fatalf("SessionID = %q, want %q", got.SessionID, tt.wantSession)
			}
			if got.HasNativeSession != (tt.wantSession != "") {
				t.Fatalf("HasNativeSession = %v, want %v", got.HasNativeSession, tt.wantSession != "")
			}
		})
	}
}
