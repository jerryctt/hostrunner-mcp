package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/jerryctt/hostrunner-mcp/internal/exec"
)

type fakeRunner struct {
	calls []exec.Request
	reply map[string]exec.Result
}

func (f *fakeRunner) Run(_ context.Context, req exec.Request) (exec.Result, error) {
	f.calls = append(f.calls, req)
	key := req.Command + " " + strings.Join(req.Args, " ")
	if r, ok := f.reply[key]; ok {
		return r, nil
	}
	return exec.Result{}, nil
}

func TestReviewArgs(t *testing.T) {
	// "" and "uncommitted" both produce ["review", "--uncommitted"]
	for _, mode := range []string{"", "uncommitted"} {
		got, positional, err := ReviewArgs(mode, "", "", "")
		if err != nil {
			t.Errorf("mode=%q: unexpected error %v", mode, err)
		}
		if strings.Join(got, " ") != "review --uncommitted" || positional != "" {
			t.Errorf("mode=%q: got %v %q, want [review --uncommitted] and no positional", mode, got, positional)
		}
	}

	// base mode -- requires base ref
	got, positional, err := ReviewArgs("base", "main", "", "")
	if err != nil {
		t.Fatalf("base mode: %v", err)
	}
	if strings.Join(got, " ") != "review --base main" || positional != "" {
		t.Errorf("base mode: got %v %q", got, positional)
	}
	if _, _, err := ReviewArgs("base", "", "", ""); err == nil {
		t.Errorf("base without ref should error")
	}

	// commit mode -- requires sha
	got, positional, err = ReviewArgs("commit", "", "abc123", "")
	if err != nil {
		t.Fatalf("commit mode: %v", err)
	}
	if strings.Join(got, " ") != "review --commit abc123" || positional != "" {
		t.Errorf("commit mode: got %v %q", got, positional)
	}
	if _, _, err := ReviewArgs("commit", "", "", ""); err == nil {
		t.Errorf("commit without sha should error")
	}

	// unknown mode
	if _, _, err := ReviewArgs("unknown-mode", "", "", ""); err == nil {
		t.Errorf("unknown mode should error")
	}
}

// The codex CLI rejects scope flags combined with the positional [PROMPT]
// ("the argument '--uncommitted' cannot be used with '[PROMPT]'"), so a
// non-empty prompt must suppress the scope flag and fold the scope into the
// prompt text instead.
func TestReviewArgsPromptSuppressesScopeFlags(t *testing.T) {
	cases := []struct {
		mode, base, commit string
		wantInPrompt       string
	}{
		{"", "", "", "uncommitted"},
		{"uncommitted", "", "", "uncommitted"},
		{"base", "main", "", "base branch main"},
		{"commit", "", "abc123", "commit abc123"},
	}
	for _, c := range cases {
		args, positional, err := ReviewArgs(c.mode, c.base, c.commit, "focus on error handling")
		if err != nil {
			t.Fatalf("mode=%q: %v", c.mode, err)
		}
		if strings.Join(args, " ") != "review" {
			t.Errorf("mode=%q: args = %v, want [review] only (no scope flag with prompt)", c.mode, args)
		}
		if !strings.Contains(positional, c.wantInPrompt) {
			t.Errorf("mode=%q: positional %q should mention %q", c.mode, positional, c.wantInPrompt)
		}
		if !strings.Contains(positional, "focus on error handling") {
			t.Errorf("mode=%q: positional %q should contain the caller prompt", c.mode, positional)
		}
	}

	// base/commit validation still applies with a prompt
	if _, _, err := ReviewArgs("base", "", "", "p"); err == nil {
		t.Errorf("base without ref should error even with prompt")
	}
	if _, _, err := ReviewArgs("commit", "", "", "p"); err == nil {
		t.Errorf("commit without sha should error even with prompt")
	}
}

func TestReviewHappyPath(t *testing.T) {
	f := &fakeRunner{reply: map[string]exec.Result{
		"codex review --uncommitted": {Stdout: "LGTM", ExitCode: 0},
	}}
	res, err := Review(context.Background(), f, "codex", nil, 0, 1000, nil, ReviewParams{
		Folder: "/repo", Mode: "uncommitted",
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Output != "LGTM" {
		t.Errorf("Output = %q, want LGTM", res.Output)
	}
	if res.Mode != "uncommitted" {
		t.Errorf("Mode = %q, want uncommitted", res.Mode)
	}

	// Verify runner was called with correct command and args
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(f.calls))
	}
	call := f.calls[0]
	if call.Command != "codex" {
		t.Errorf("Command = %q, want codex", call.Command)
	}
	hasReview := false
	hasUncommitted := false
	for _, a := range call.Args {
		if a == "review" {
			hasReview = true
		}
		if a == "--uncommitted" {
			hasUncommitted = true
		}
		if a == "--write" || a == "--full-auto" {
			t.Errorf("read-only check: got disallowed arg %q", a)
		}
	}
	if !hasReview || !hasUncommitted {
		t.Errorf("Args = %v, want to contain 'review' and '--uncommitted'", call.Args)
	}

	// No git work (stdin should be empty)
	if call.Stdin != "" {
		t.Errorf("Stdin should be empty (no diff piping), got %q", call.Stdin)
	}
}

func TestReviewBaseMode(t *testing.T) {
	f := &fakeRunner{reply: map[string]exec.Result{
		"codex review --base main": {Stdout: "looks good", ExitCode: 0},
	}}
	res, err := Review(context.Background(), f, "codex", nil, 0, 1000, nil, ReviewParams{
		Folder: "/repo", Mode: "base", Base: "main",
	})
	if err != nil {
		t.Fatalf("Review base mode: %v", err)
	}
	if res.Output != "looks good" {
		t.Errorf("Output = %q, want 'looks good'", res.Output)
	}
	if res.Mode != "base" {
		t.Errorf("Mode = %q, want base", res.Mode)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(f.calls))
	}
	args := f.calls[0].Args
	if strings.Join(args, " ") != "review --base main" {
		t.Errorf("Args = %v, want [review --base main]", args)
	}
}

func TestReviewExtraArgs(t *testing.T) {
	f := &fakeRunner{reply: map[string]exec.Result{
		"codex review --uncommitted -c model=o3": {Stdout: "ok", ExitCode: 0},
	}}
	res, err := Review(context.Background(), f, "codex", []string{"-c", "model=o3"}, 0, 1000, nil, ReviewParams{
		Folder: "/repo", Mode: "uncommitted",
	})
	if err != nil {
		t.Fatalf("Review extra args: %v", err)
	}
	if res.Output != "ok" {
		t.Errorf("Output = %q, want ok", res.Output)
	}
}

func TestReviewWithPrompt(t *testing.T) {
	f := &fakeRunner{}
	res, err := Review(context.Background(), f, "codex", nil, 0, 1000, nil, ReviewParams{
		Folder: "/repo", Mode: "uncommitted", Prompt: "focus on error handling",
	})
	if err != nil {
		t.Fatalf("Review with prompt: %v", err)
	}
	if res.Mode != "uncommitted" {
		t.Errorf("Mode = %q, want uncommitted", res.Mode)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(f.calls))
	}
	args := f.calls[0].Args
	// codex rejects --uncommitted together with [PROMPT]; the prompt must be
	// the only argument after "review", with the scope folded into it.
	if len(args) != 2 || args[0] != "review" {
		t.Fatalf("args = %v, want [review <prompt>]", args)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			t.Errorf("args = %v: no flags allowed alongside a prompt", args)
		}
	}
	if !strings.Contains(args[1], "uncommitted") || !strings.Contains(args[1], "focus on error handling") {
		t.Errorf("prompt arg = %q, want scope sentence + caller prompt", args[1])
	}
}

func TestReviewModeEmptyLabel(t *testing.T) {
	f := &fakeRunner{reply: map[string]exec.Result{
		"codex review --uncommitted": {Stdout: "fine", ExitCode: 0},
	}}
	res, err := Review(context.Background(), f, "codex", nil, 0, 1000, nil, ReviewParams{
		Folder: "/repo", Mode: "",
	})
	if err != nil {
		t.Fatalf("Review empty mode: %v", err)
	}
	// modeLabel("") should return "uncommitted"
	if res.Mode != "uncommitted" {
		t.Errorf("Mode = %q, want uncommitted", res.Mode)
	}
}
