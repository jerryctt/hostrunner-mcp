package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerryctt/hostrunner-mcp/internal/config"
	"github.com/jerryctt/hostrunner-mcp/internal/exec"
	"github.com/rs/zerolog"
)

type fakeRunner struct{ reply map[string]exec.Result }

func (f *fakeRunner) Run(_ context.Context, req exec.Request) (exec.Result, error) {
	key := req.Command + " " + strings.Join(req.Args, " ")
	return f.reply[key], nil
}

func newCfg(t *testing.T) (*config.Config, string) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		AllowedRoots:    []string{root},
		AllowedCommands: []string{"codex", "git"},
		CodexExtraArgs:  nil,
		MaxOutputBytes:  1000,
	}, repo
}

func TestHandleCodexReviewRejectsOutsideRoot(t *testing.T) {
	cfg, _ := newCfg(t)
	r := &fakeRunner{}
	out, isErr := HandleCodexReview(context.Background(), cfg, r, zerolog.Nop(), "/etc", "uncommitted", "", "")
	if !isErr || !strings.Contains(out, "allowed_root") {
		t.Errorf("expected allowlist rejection, got %q isErr=%v", out, isErr)
	}
}

func TestHandleRunCommandRejectsDisallowedCommand(t *testing.T) {
	cfg, repo := newCfg(t)
	r := &fakeRunner{}
	out, isErr := HandleRunCommand(context.Background(), cfg, r, zerolog.Nop(), "rm", []string{"-rf", "/"}, repo)
	if !isErr || !strings.Contains(out, "not allowed") {
		t.Errorf("expected command rejection, got %q isErr=%v", out, isErr)
	}
}

func TestHandleRunCommandHappyPath(t *testing.T) {
	cfg, repo := newCfg(t)
	r := &fakeRunner{reply: map[string]exec.Result{"git status": {Stdout: "clean", ExitCode: 0}}}
	out, isErr := HandleRunCommand(context.Background(), cfg, r, zerolog.Nop(), "git", []string{"status"}, repo)
	if isErr || !strings.Contains(out, "clean") {
		t.Errorf("expected success with output, got %q isErr=%v", out, isErr)
	}
}
