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
		got, err := ReviewArgs(mode, "", "")
		if err != nil {
			t.Errorf("mode=%q: unexpected error %v", mode, err)
		}
		if strings.Join(got, " ") != "review --uncommitted" {
			t.Errorf("mode=%q: got %v, want [review --uncommitted]", mode, got)
		}
	}

	// base mode -- requires base ref
	got, err := ReviewArgs("base", "main", "")
	if err != nil {
		t.Fatalf("base mode: %v", err)
	}
	if strings.Join(got, " ") != "review --base main" {
		t.Errorf("base mode: got %v", got)
	}
	if _, err := ReviewArgs("base", "", ""); err == nil {
		t.Errorf("base without ref should error")
	}

	// commit mode -- requires sha
	got, err = ReviewArgs("commit", "", "abc123")
	if err != nil {
		t.Fatalf("commit mode: %v", err)
	}
	if strings.Join(got, " ") != "review --commit abc123" {
		t.Errorf("commit mode: got %v", got)
	}
	if _, err := ReviewArgs("commit", "", ""); err == nil {
		t.Errorf("commit without sha should error")
	}

	// unknown mode
	if _, err := ReviewArgs("unknown-mode", "", ""); err == nil {
		t.Errorf("unknown mode should error")
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
