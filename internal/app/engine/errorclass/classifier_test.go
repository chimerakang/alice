package errorclass

import (
	"errors"
	"testing"
)

func TestClassifyText_PatternCoverage(t *testing.T) {
	// Every pattern from the original two classifiers must map to the
	// expected Class. Failure here means we lost coverage during the
	// migration — production log analysis would silently start treating
	// known errors as Unknown.
	cases := []struct {
		text string
		want Class
	}{
		// engine.isTransientRecoveryError patterns
		{"rate limit hit", ClassRateLimit},
		{"HTTP 429 too many requests", ClassRateLimit},
		{"server overloaded", ClassServerOverload},
		{"529 server error", ClassServerOverload},
		{"temporary failure", ClassEOF},
		{"connection reset by peer", ClassNetworkReset},
		{"unexpected EOF", ClassEOF},

		// hermes.ClassifyFailure env patterns (extends the above)
		{"prompt is too long", ClassContextTooLong},
		{"exceeds context length", ClassContextTooLong},
		{"request entity too large", ClassContextTooLong},
		{"context_length_exceeded", ClassContextTooLong},
		{"context deadline exceeded", ClassTimeout},
		{"context deadline", ClassTimeout},
		{"i/o timeout", ClassTimeout},
		{"connection refused", ClassNetworkReset},
		{"connection timed out", ClassTimeout},
		{"no such host", ClassNetworkReset},
		{"tls handshake failure", ClassNetworkReset},
		{"signal: killed", ClassKilled},
		{"503 Service Unavailable", ClassServerOverload},
		{"504 Gateway Timeout", ClassServerOverload},
		{"upstream connect error", ClassServerOverload},

		// Edge cases
		{"", ClassUnknown},
		{"   ", ClassUnknown},
		{"some random executor failure with no env keyword", ClassUnknown},
	}
	for _, c := range cases {
		got := ClassifyText(c.text)
		if got != c.want {
			t.Errorf("ClassifyText(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestClassify_NilError(t *testing.T) {
	if got := Classify(nil); got != ClassUnknown {
		t.Errorf("Classify(nil) = %q, want %q", got, ClassUnknown)
	}
}

func TestClassify_WrappedError(t *testing.T) {
	err := errors.New("connection reset by peer")
	if got := Classify(err); got != ClassNetworkReset {
		t.Errorf("Classify(%v) = %q, want %q", err, got, ClassNetworkReset)
	}
}

func TestClassifyText_CaseInsensitive(t *testing.T) {
	cases := []string{"RATE LIMIT", "Rate Limit", "rate limit", "rAtE LiMiT"}
	for _, s := range cases {
		if got := ClassifyText(s); got != ClassRateLimit {
			t.Errorf("ClassifyText(%q) = %q, want %q", s, got, ClassRateLimit)
		}
	}
}

func TestIsTransient(t *testing.T) {
	transient := []Class{ClassRateLimit, ClassServerOverload, ClassNetworkReset, ClassTimeout, ClassEOF}
	notTransient := []Class{ClassUnknown, ClassContextTooLong, ClassKilled}
	for _, c := range transient {
		if !c.IsTransient() {
			t.Errorf("%q.IsTransient() = false, want true", c)
		}
	}
	for _, c := range notTransient {
		if c.IsTransient() {
			t.Errorf("%q.IsTransient() = true, want false", c)
		}
	}
}

func TestIsEnv(t *testing.T) {
	env := []Class{
		ClassRateLimit, ClassServerOverload, ClassNetworkReset, ClassTimeout, ClassEOF,
		ClassContextTooLong, ClassKilled,
	}
	notEnv := []Class{ClassUnknown}
	for _, c := range env {
		if !c.IsEnv() {
			t.Errorf("%q.IsEnv() = false, want true", c)
		}
	}
	for _, c := range notEnv {
		if c.IsEnv() {
			t.Errorf("%q.IsEnv() = true, want false", c)
		}
	}
}

func TestIsRetryable_AliasesIsTransient(t *testing.T) {
	all := []Class{ClassUnknown, ClassRateLimit, ClassServerOverload, ClassNetworkReset, ClassTimeout, ClassEOF, ClassContextTooLong, ClassKilled}
	for _, c := range all {
		if c.IsRetryable() != c.IsTransient() {
			t.Errorf("%q.IsRetryable() = %v but IsTransient() = %v", c, c.IsRetryable(), c.IsTransient())
		}
	}
}

// Regression: every original engine.isTransientRecoveryError pattern must
// still be recognised as transient via the unified classifier.
func TestRegression_IsTransientRecoveryErrorPatterns(t *testing.T) {
	originalPatterns := []string{
		"rate limit",
		"429",
		"overloaded",
		"529",
		"temporary",
		"connection reset",
		"eof",
	}
	for _, p := range originalPatterns {
		if !ClassifyText(p).IsTransient() {
			t.Errorf("legacy transient pattern %q is no longer recognised as transient (class=%q)", p, ClassifyText(p))
		}
	}
}

// Regression: every original hermes.envFailurePatterns entry must still be
// recognised as env via the unified classifier.
func TestRegression_HermesEnvFailurePatterns(t *testing.T) {
	originalPatterns := []string{
		"prompt is too long",
		"context length",
		"request entity too large",
		"context_length_exceeded",
		"deadline exceeded",
		"context deadline",
		"i/o timeout",
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"tls handshake",
		"eof",
		"signal: killed",
		"rate limit",
		"too many requests",
		"503 service unavailable",
		"504 gateway timeout",
		"upstream connect error",
	}
	for _, p := range originalPatterns {
		if !ClassifyText(p).IsEnv() {
			t.Errorf("legacy env pattern %q is no longer recognised as env (class=%q)", p, ClassifyText(p))
		}
	}
}
