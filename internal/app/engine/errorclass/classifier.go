// Package errorclass is the single source of truth for classifying error
// strings produced by Claude/Codex CLI subprocesses, model APIs, the network
// layer, and Alice's own runtime.
//
// Before this package, Alice had two independent string classifiers:
//
//   - engine.isTransientRecoveryError — answered "should we retry?"
//   - hermes.ClassifyFailure          — answered "env error or content error?"
//
// They overlapped on a handful of patterns (rate limit, connection reset,
// EOF) and disagreed on dozens of others (e.g. ClassifyFailure recognised
// "tls handshake" and "prompt is too long", isTransientRecoveryError did
// not). The disagreement was not deliberate — it was the natural drift of
// two parallel pattern lists evolving in different files. Production logs
// that one classifier called "retryable" the other could mark "content".
//
// This package collapses the two pattern lists into one Class enum plus two
// predicates (IsTransient, IsEnv) that recover each original question
// without losing pattern coverage. New error categories are added here and
// only here; callers stay thin wrappers.
//
// # Architectural boundary (#169 #6)
//
// errorclass owns one job: substring → Class for raw error strings.
// Other error-handling code in the codebase has different jobs and
// MUST NOT grow its own pattern list — they delegate to ClassifyText
// or use typed errors instead:
//
//   - engine.handlePlanningError uses typed errors
//     (hermes.ErrPlannerJSONFailed / ErrPlannerEmptyPlan) — the type
//     IS the classification. Not a string classifier.
//   - hermes.PlannerSession.Plan retries JSON parse failures via its
//     own internal counter (MaxPlannerJSONRetries). The "is JSON
//     valid?" question is local; transient/env classification of the
//     planner's transport errors uses errorclass via the recovery
//     decider.
//   - app.classifyError (Telegram-side UX label) layers UX-only
//     categories (cancelled, tool_file_patch, permission, not_found)
//     on top of errorclass for the transient/env subset; it does
//     not re-implement the transient pattern list.
//   - engine.isTransientRecoveryError + hermes.ClassifyFailure are
//     thin wrappers around ClassifyText.IsTransient / IsEnv.
//
// If you find yourself reaching for `strings.Contains(err.Error(),
// "rate limit")` or similar in a new file, add a Class here instead
// and let callers compose. Drift between parallel pattern lists is
// the bug this package was created to prevent.
package errorclass

import "strings"

// Class is the canonical category for an error string. Each value covers a
// disjoint set of substring patterns; see ClassifyText for the mapping.
type Class string

const (
	// ClassUnknown is the fallback when no pattern matches. Treat as a
	// content/code-level error: not retryable, not env-related.
	ClassUnknown Class = "unknown"

	// ClassRateLimit covers HTTP 429 / "rate limit" / "too many requests".
	// Retry usually succeeds after backoff.
	ClassRateLimit Class = "rate_limit"

	// ClassServerOverload covers 5xx server-side issues (503/504/529,
	// "overloaded", upstream connect errors). Retry usually succeeds.
	ClassServerOverload Class = "server_overload"

	// ClassNetworkReset covers L4 connection failures: reset, refused,
	// host resolution, TLS handshake. Retry may succeed.
	ClassNetworkReset Class = "network_reset"

	// ClassTimeout covers "deadline exceeded" / "i/o timeout" /
	// "connection timed out". Retry may succeed if the cause was load.
	ClassTimeout Class = "timeout"

	// ClassEOF covers premature stream termination signals; treated as
	// transient because it usually reflects upstream churn.
	ClassEOF Class = "eof"

	// ClassContextTooLong covers prompt size / context window overflow.
	// Env-related (caller fixes by trimming context) but NOT transient —
	// retrying the same prompt will fail again.
	ClassContextTooLong Class = "context_too_long"

	// ClassKilled covers "signal: killed" — usually OOM kill or supervisor
	// kill. Env-related but not transient; need investigation.
	ClassKilled Class = "killed"
)

// transientPatterns / nonTransientEnvPatterns split the original two
// classifiers' substring lists by Class. Lower-case substring match.
var classPatterns = []struct {
	patterns []string
	class    Class
}{
	{[]string{"rate limit", "too many requests", "429"}, ClassRateLimit},
	{[]string{"overloaded", "529", "503 service unavailable", "504 gateway timeout", "upstream connect error"}, ClassServerOverload},
	{[]string{"connection reset", "connection refused", "no such host", "tls handshake"}, ClassNetworkReset},
	{[]string{"deadline exceeded", "context deadline", "i/o timeout", "connection timed out"}, ClassTimeout},
	{[]string{"eof", "temporary"}, ClassEOF},
	{[]string{"prompt is too long", "context length", "request entity too large", "context_length_exceeded"}, ClassContextTooLong},
	{[]string{"signal: killed"}, ClassKilled},
}

// Classify maps a Go error to a Class. nil maps to ClassUnknown.
func Classify(err error) Class {
	if err == nil {
		return ClassUnknown
	}
	return ClassifyText(err.Error())
}

// ClassifyText is the substring-matching primitive. Empty input maps to
// ClassUnknown. The first matching pattern wins; pattern order is the
// declaration order in classPatterns.
func ClassifyText(text string) Class {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return ClassUnknown
	}
	for _, group := range classPatterns {
		for _, p := range group.patterns {
			if strings.Contains(s, p) {
				return group.class
			}
		}
	}
	return ClassUnknown
}

// IsTransient reports whether retrying the same operation is likely to
// succeed. Recovers the original engine.isTransientRecoveryError semantics:
// rate limit, server overload, network reset, timeout, EOF.
//
// ClassContextTooLong and ClassKilled are env-related but NOT transient —
// the caller must change something (shrink prompt, investigate kill cause)
// before retrying.
func (c Class) IsTransient() bool {
	switch c {
	case ClassRateLimit, ClassServerOverload, ClassNetworkReset, ClassTimeout, ClassEOF:
		return true
	}
	return false
}

// IsEnv reports whether the error came from the CLI / network / supervisor
// layer rather than the executor's own work. Recovers the original
// hermes.ClassifyFailure semantics: env vs content.
//
// All transient classes are env. ContextTooLong and Killed are env but not
// transient. Unknown is treated as content (the executor's actual work).
func (c Class) IsEnv() bool {
	if c.IsTransient() {
		return true
	}
	switch c {
	case ClassContextTooLong, ClassKilled:
		return true
	}
	return false
}

// IsRetryable is an alias for IsTransient kept for caller readability when
// the question is framed as "retry now?" rather than "is this transient?".
func (c Class) IsRetryable() bool {
	return c.IsTransient()
}
