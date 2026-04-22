package hermes

import (
	"context"
	"testing"
)

func TestPathGuard_BlocksConfigJSON(t *testing.T) {
	hook := PathGuard()
	ctx := context.Background()

	cases := []struct {
		tool    string
		args    map[string]any
		wantErr bool
	}{
		// Blocked paths
		{tool: "Edit", args: map[string]any{"file_path": "config.json"}, wantErr: true},
		{tool: "Write", args: map[string]any{"file_path": "config.json"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": "/project/.git/config"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": ".env"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": ".env.local"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": ".env.production"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": "id_rsa"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": "server.pem"}, wantErr: true},
		{tool: "Edit", args: map[string]any{"file_path": "private.key"}, wantErr: true},
		// Allowed paths
		{tool: "Edit", args: map[string]any{"file_path": "main.go"}, wantErr: false},
		{tool: "Read", args: map[string]any{"file_path": "config.json"}, wantErr: false}, // reads are fine
		{tool: "Edit", args: map[string]any{"file_path": "config.example.json"}, wantErr: false},
	}

	for _, tc := range cases {
		err := hook(ctx, tc.tool, tc.args)
		if tc.wantErr && err == nil {
			t.Errorf("tool=%s args=%v: expected error, got nil", tc.tool, tc.args)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("tool=%s args=%v: unexpected error: %v", tc.tool, tc.args, err)
		}
	}
}

func TestPathGuard_ExtraPatterns(t *testing.T) {
	hook := PathGuard("secrets.yaml", "*.bak")
	ctx := context.Background()

	if err := hook(ctx, "Edit", map[string]any{"file_path": "secrets.yaml"}); err == nil {
		t.Error("expected error for secrets.yaml, got nil")
	}
	if err := hook(ctx, "Edit", map[string]any{"file_path": "backup.bak"}); err == nil {
		t.Error("expected error for *.bak match, got nil")
	}
	if err := hook(ctx, "Edit", map[string]any{"file_path": "main.go"}); err != nil {
		t.Errorf("unexpected error for main.go: %v", err)
	}
}

func TestMatchesDenyList(t *testing.T) {
	patterns := []string{"config.json", ".git/", ".env", "*.pem"}

	cases := []struct {
		path    string
		blocked bool
	}{
		{"config.json", true},
		{"/abs/path/config.json", true},
		{".git/config", true},
		{"/repo/.git/hooks/pre-commit", true},
		{".env", true},
		{"server.pem", true},
		{"main.go", false},
		{"config.example.json", false},
		{".gitignore", false},
	}

	for _, tc := range cases {
		blocked, _ := matchesDenyList(tc.path, patterns)
		if blocked != tc.blocked {
			t.Errorf("matchesDenyList(%q): got %v, want %v", tc.path, blocked, tc.blocked)
		}
	}
}

func TestHookRegistry_RunPre_StopsOnFirstError(t *testing.T) {
	reg := NewHookRegistry()
	calls := 0

	reg.RegisterPre(func(ctx context.Context, tool string, args map[string]any) error {
		calls++
		return factError("blocked by hook 1")
	})
	reg.RegisterPre(func(ctx context.Context, tool string, args map[string]any) error {
		calls++
		return nil
	})

	err := reg.RunPre(context.Background(), "Edit", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("expected 1 hook call, got %d", calls)
	}
}

func TestHookRegistry_RunPost_CollectsErrors(t *testing.T) {
	reg := NewHookRegistry()

	reg.RegisterPost(func(ctx context.Context, tool string, args, result map[string]any) error {
		return factError("validator A failed")
	})
	reg.RegisterPost(func(ctx context.Context, tool string, args, result map[string]any) error {
		return factError("validator B failed")
	})

	result := map[string]any{}
	reg.RunPost(context.Background(), "Edit", nil, result)

	ve, ok := result["validation_error"]
	if !ok {
		t.Fatal("expected validation_error in result")
	}
	s := ve.(string)
	if s == "" {
		t.Error("validation_error should not be empty")
	}
}

func TestHookRegistry_RunPost_NoErrorWhenAllPass(t *testing.T) {
	reg := NewHookRegistry()

	reg.RegisterPost(func(ctx context.Context, tool string, args, result map[string]any) error {
		return nil
	})

	result := map[string]any{}
	reg.RunPost(context.Background(), "Edit", nil, result)

	if _, ok := result["validation_error"]; ok {
		t.Error("validation_error should not be set when all hooks pass")
	}
}

func TestBuildRegistry_PathGuardDisabledByDefault(t *testing.T) {
	cfg := HooksConfig{PathGuard: false}
	reg := BuildRegistry(cfg, "/tmp")

	err := reg.RunPre(context.Background(), "Edit", map[string]any{"file_path": "config.json"})
	if err != nil {
		t.Errorf("expected no error when path_guard disabled, got: %v", err)
	}
}

func TestBuildRegistry_PathGuardEnabled(t *testing.T) {
	cfg := HooksConfig{PathGuard: true}
	reg := BuildRegistry(cfg, "/tmp")

	err := reg.RunPre(context.Background(), "Edit", map[string]any{"file_path": "config.json"})
	if err == nil {
		t.Error("expected error when path_guard enabled and writing config.json")
	}
}

func TestIsGoFileWrite(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want bool
	}{
		{"Edit", map[string]any{"file_path": "main.go"}, true},
		{"Write", map[string]any{"file_path": "pkg/foo.go"}, true},
		{"Edit", map[string]any{"file_path": "main.ts"}, false},
		{"Read", map[string]any{"file_path": "main.go"}, false},
		{"Edit", map[string]any{"file_path": "README.md"}, false},
	}

	for _, tc := range cases {
		got := isGoFileWrite(tc.tool, tc.args)
		if got != tc.want {
			t.Errorf("isGoFileWrite(%q, %v) = %v, want %v", tc.tool, tc.args, got, tc.want)
		}
	}
}

func TestIsTsFileWrite(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want bool
	}{
		{"Edit", map[string]any{"file_path": "app.ts"}, true},
		{"Write", map[string]any{"file_path": "comp.tsx"}, true},
		{"Edit", map[string]any{"file_path": "main.go"}, false},
		{"Read", map[string]any{"file_path": "app.ts"}, false},
	}

	for _, tc := range cases {
		got := isTsFileWrite(tc.tool, tc.args)
		if got != tc.want {
			t.Errorf("isTsFileWrite(%q, %v) = %v, want %v", tc.tool, tc.args, got, tc.want)
		}
	}
}

func TestTrimLines(t *testing.T) {
	lines := "a\nb\nc\nd\ne"
	got := trimLines(lines, 3)
	wantPrefix := "a\nb\nc\n"
	if len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("trimLines: got %q, want prefix %q", got, wantPrefix)
	}
}
