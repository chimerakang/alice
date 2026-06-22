package engine

// SessionPolicy names how a model call should combine native backend session
// state with prompt-injected memory. It is deliberately separate from
// MemoryResolver: policy decides whether memory is allowed; resolver only
// renders allowed sources.
type SessionPolicy string

const (
	SessionPolicyNativeOnly      SessionPolicy = "native_only"
	SessionPolicyMemoryOnly      SessionPolicy = "memory_only"
	SessionPolicyHybridIssueOnly SessionPolicy = "hybrid_issue_only"
	SessionPolicyFresh           SessionPolicy = "fresh"
)

type SessionPolicyRequest struct {
	Mode             string
	HasNativeSession bool
	HasIssueScope    bool
}

type SessionPolicyDecision struct {
	Policy             SessionPolicy
	UseNativeSession   bool
	AllowRecentMemory  bool
	AllowGeneralMemory bool
	AllowStaticHints   bool
	Reason             string
}

// SessionRunDecision is the shared run-level decision shape for a direct model
// call. It combines model routing with backend/native-session selection so the
// caller can pass one object through execution.
type SessionRunDecision struct {
	Model            string
	RoutingReason    string
	LatencyMS        int
	Backend          BackendKind
	PreviousBackend  BackendKind
	SessionID        string
	HasNativeSession bool
}

type SessionRunRequest struct {
	OverrideModel     string
	StickyModel       string
	ContinuationModel string
	RoutedModel       string
	RoutedReason      string
	RoutedLatencyMS   int
	DefaultModel      string
	PreviousBackend   BackendKind
	BackendSessions   map[BackendKind]string
}

// DecideSessionRun applies direct-run model priority and native-session lookup
// as a pure input/output decision. Callers gather facts; this function owns the
// ordering: explicit override, sticky session, follow-up continuation, routed
// model, then client default.
func DecideSessionRun(req SessionRunRequest) SessionRunDecision {
	decision := SessionRunDecision{
		PreviousBackend: req.PreviousBackend,
	}

	switch {
	case req.OverrideModel != "":
		decision.Model = req.OverrideModel
		decision.RoutingReason = "user_command"
	case req.StickyModel != "":
		decision.Model = req.StickyModel
		decision.RoutingReason = "sticky_session"
	case req.ContinuationModel != "":
		decision.Model = req.ContinuationModel
		decision.RoutingReason = "follow_up"
	case req.RoutedModel != "":
		decision.Model = req.RoutedModel
		decision.RoutingReason = req.RoutedReason
		decision.LatencyMS = req.RoutedLatencyMS
	default:
		decision.Model = req.DefaultModel
		decision.RoutingReason = "default"
	}
	if decision.RoutingReason == "" {
		decision.RoutingReason = "default"
	}

	decision.Backend = BackendKindForModel(decision.Model)
	if req.BackendSessions != nil {
		decision.SessionID = req.BackendSessions[decision.Backend]
	}
	decision.HasNativeSession = decision.SessionID != ""
	return decision
}

// DecideSessionPolicy centralizes the first slice of session/memory policy.
// The current implementation preserves existing bridge behavior while making
// it testable: direct resume/model-switch/continuation bridges trust only
// same-chat recent messages, avoiding broad semantic recall for low-signal
// follow-ups (#145/#146).
func DecideSessionPolicy(req SessionPolicyRequest) SessionPolicyDecision {
	switch req.Mode {
	case "direct_resume_fallback":
		return SessionPolicyDecision{
			Policy:            SessionPolicyMemoryOnly,
			UseNativeSession:  false,
			AllowRecentMemory: !req.HasIssueScope,
			Reason:            "native_resume_unavailable",
		}
	case "direct_model_switch", "direct_continuation":
		return SessionPolicyDecision{
			Policy:            SessionPolicyMemoryOnly,
			UseNativeSession:  false,
			AllowRecentMemory: !req.HasIssueScope,
			Reason:            "trusted_recent_bridge",
		}
	default:
		if req.HasIssueScope {
			return SessionPolicyDecision{
				Policy:             SessionPolicyHybridIssueOnly,
				UseNativeSession:   req.HasNativeSession,
				AllowGeneralMemory: true,
				AllowStaticHints:   true,
				Reason:             "explicit_issue_scope",
			}
		}
		return SessionPolicyDecision{
			Policy:             SessionPolicyMemoryOnly,
			UseNativeSession:   false,
			AllowRecentMemory:  true,
			AllowGeneralMemory: true,
			AllowStaticHints:   true,
			Reason:             "memory_bundle",
		}
	}
}
