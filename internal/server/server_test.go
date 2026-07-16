package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	out, isErr := HandleCodexReview(context.Background(), cfg, r, zerolog.Nop(), "/etc", "uncommitted", "", "", "")
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

func TestHandleCodexReviewOmitsStderrOnSuccess(t *testing.T) {
	cfg, repo := newCfg(t)
	r := &fakeRunner{reply: map[string]exec.Result{
		"codex review --uncommitted": {Stdout: "VERDICT_OK", Stderr: "NOISE_TRACE", ExitCode: 0},
	}}
	out, isErr := HandleCodexReview(context.Background(), cfg, r, zerolog.Nop(), repo, "uncommitted", "", "", "")
	if isErr {
		t.Fatalf("expected success, got error: %q", out)
	}
	if !strings.Contains(out, "VERDICT_OK") {
		t.Errorf("expected output to contain VERDICT_OK, got: %q", out)
	}
	if !strings.Contains(out, "codex exit 0") {
		t.Errorf("expected output to contain 'codex exit 0', got: %q", out)
	}
	if strings.Contains(out, "NOISE_TRACE") {
		t.Errorf("expected output to NOT contain NOISE_TRACE, got: %q", out)
	}
	if strings.Contains(out, "--- codex stderr") {
		t.Errorf("expected output to NOT contain '--- codex stderr', got: %q", out)
	}
}

func waitStatus(t *testing.T, jobs *jobStore, id string, wait time.Duration) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, isErr := HandleReviewStatus(context.Background(), jobs, id, wait)
		if isErr || !strings.Contains(out, "status: running") {
			return out, isErr
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not finish in time", id)
		}
	}
}

func TestReviewStartAndStatus(t *testing.T) {
	cfg, repo := newCfg(t)
	r := &fakeRunner{reply: map[string]exec.Result{
		"codex review --uncommitted": {Stdout: "VERDICT_OK", ExitCode: 0},
	}}
	jobs := newJobStore()

	out, isErr := HandleReviewStart(cfg, r, zerolog.Nop(), jobs, repo, "uncommitted", "", "", "")
	if isErr {
		t.Fatalf("start: %q", out)
	}
	if !strings.Contains(out, "job_id: ") {
		t.Fatalf("start output missing job_id: %q", out)
	}
	id := strings.TrimSpace(strings.Split(strings.SplitAfter(out, "job_id: ")[1], "\n")[0])

	got, isErr := waitStatus(t, jobs, id, 100*time.Millisecond)
	if isErr {
		t.Fatalf("status: %q", got)
	}
	if !strings.Contains(got, "status: completed") || !strings.Contains(got, "VERDICT_OK") {
		t.Errorf("status = %q, want completed with review output", got)
	}

	// Result stays retrievable after completion.
	again, isErr := HandleReviewStatus(context.Background(), jobs, id, time.Millisecond)
	if isErr || !strings.Contains(again, "VERDICT_OK") {
		t.Errorf("re-fetch = %q isErr=%v, want cached result", again, isErr)
	}
}

func TestReviewStartValidatesUpfront(t *testing.T) {
	cfg, repo := newCfg(t)
	jobs := newJobStore()

	// Outside allowed roots.
	out, isErr := HandleReviewStart(cfg, &fakeRunner{}, zerolog.Nop(), jobs, "/etc", "uncommitted", "", "", "")
	if !isErr || !strings.Contains(out, "allowed_root") {
		t.Errorf("expected allowlist rejection, got %q isErr=%v", out, isErr)
	}

	// Bad scope params fail immediately, not in the background job.
	out, isErr = HandleReviewStart(cfg, &fakeRunner{}, zerolog.Nop(), jobs, repo, "base", "", "", "")
	if !isErr || !strings.Contains(out, "base") {
		t.Errorf("expected base validation error, got %q isErr=%v", out, isErr)
	}
}

func TestReviewStartRejectsConcurrentJobForSameWorktree(t *testing.T) {
	cfg, repo := newCfg(t)
	// Make repo look like a git worktree with subdirectories.
	for _, d := range []string{".git", "internal", "cmd"} {
		if err := os.Mkdir(filepath.Join(repo, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	jobs := newJobStore()
	// A job started from repo/internal that never finishes.
	j, err := jobs.add(mustResolve(t, cfg, filepath.Join(repo, "internal")), "uncommitted")
	if err != nil {
		t.Fatal(err)
	}

	// Same folder, the repo root, and a sibling subdirectory are all the same
	// worktree and must be rejected while the job is running.
	for _, dir := range []string{filepath.Join(repo, "internal"), repo, filepath.Join(repo, "cmd")} {
		out, isErr := HandleReviewStart(cfg, &fakeRunner{}, zerolog.Nop(), jobs, dir, "uncommitted", "", "", "")
		if !isErr || !strings.Contains(out, "already running") || !strings.Contains(out, j.ID) {
			t.Errorf("dir %s: expected duplicate-job rejection mentioning %s, got %q isErr=%v", dir, j.ID, out, isErr)
		}
	}

	// Once finished, a new review for the same worktree is allowed.
	j.finish("done", false)
	out, isErr := HandleReviewStart(cfg, &fakeRunner{}, zerolog.Nop(), jobs, repo, "uncommitted", "", "", "")
	if isErr {
		t.Errorf("expected new job after previous finished, got %q", out)
	}
}

func TestJobStoreExpiresFinishedJobsOnGet(t *testing.T) {
	jobs := newJobStore()
	j, err := jobs.add("/repo", "uncommitted")
	if err != nil {
		t.Fatal(err)
	}
	j.finish("done", false)
	j.ended = time.Now().Add(-keepFinished - time.Minute)

	if _, ok := jobs.get(j.ID); ok {
		t.Errorf("job older than keepFinished should be expired on get")
	}
}

func mustResolve(t *testing.T, cfg *config.Config, dir string) string {
	t.Helper()
	resolved, err := cfg.ResolveAllowedDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// blockingRunner blocks until its context is cancelled, like a codex process
// that only dies when its process group is killed.
type blockingRunner struct{ started chan struct{} }

func (b *blockingRunner) Run(ctx context.Context, _ exec.Request) (exec.Result, error) {
	close(b.started)
	<-ctx.Done()
	return exec.Result{TimedOut: true, ExitCode: -1}, nil
}

func TestJobStoreShutdownCancelsRunningJobs(t *testing.T) {
	cfg, repo := newCfg(t)
	jobs := newJobStore()
	r := &blockingRunner{started: make(chan struct{})}

	out, isErr := HandleReviewStart(cfg, r, zerolog.Nop(), jobs, repo, "uncommitted", "", "", "")
	if isErr {
		t.Fatalf("start: %q", out)
	}
	<-r.started

	done := make(chan struct{})
	go func() { jobs.shutdown(2 * time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not return; running job was not cancelled")
	}
}

func TestReviewStatusUnknownJob(t *testing.T) {
	out, isErr := HandleReviewStatus(context.Background(), newJobStore(), "nope", time.Millisecond)
	if !isErr || !strings.Contains(out, "unknown job_id") {
		t.Errorf("expected unknown job error, got %q isErr=%v", out, isErr)
	}
}

func TestHandleCodexReviewIncludesStderrOnFailure(t *testing.T) {
	cfg, repo := newCfg(t)
	r := &fakeRunner{reply: map[string]exec.Result{
		"codex review --uncommitted": {Stdout: "partial", Stderr: "BOOM", ExitCode: 2},
	}}
	out, isErr := HandleCodexReview(context.Background(), cfg, r, zerolog.Nop(), repo, "uncommitted", "", "", "")
	if isErr {
		t.Fatalf("expected success (non-error tool result), got error: %q", out)
	}
	if !strings.Contains(out, "BOOM") {
		t.Errorf("expected output to contain BOOM, got: %q", out)
	}
	if !strings.Contains(out, "codex exit 2") {
		t.Errorf("expected output to contain 'codex exit 2', got: %q", out)
	}
}
