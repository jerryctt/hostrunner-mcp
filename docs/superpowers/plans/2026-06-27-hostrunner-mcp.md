# hostrunner-mcp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go stdio MCP server that runs on the host so Claude Cowork can trigger read-only codex reviews (and other allowlisted commands) against mounted project folders, enabling an edit→review→edit loop.

**Architecture:** A single static Go binary registered as a local stdio MCP connector in Claude Desktop. It exposes two tools — `codex_review` (flagship, read-only) and `run_command` (narrow, allowlisted argv). All execution goes through one executor that runs argv arrays via `os/exec` (never a shell), inside directories validated against an allowlist of root paths, with per-command timeouts and output caps.

**Tech Stack:** Go, `github.com/mark3labs/mcp-go` (MCP SDK), `gopkg.in/yaml.v3` (config), `github.com/rs/zerolog` (audit log). Build/release via Makefile + goreleaser. Standard `go test` for tests.

## Global Constraints

- Module path: `github.com/jerryctt/hostrunner-mcp` (adjust if the GitHub owner differs).
- Binary name: `hostrunner` (built from `cmd/hostrunner`).
- Go version floor: `1.23` — bump if `go get github.com/mark3labs/mcp-go` reports a higher floor.
- **No shell, ever.** Commands run as argv arrays via `os/exec`; no `sh -c`, no pipes/redirects in production code.
- **Host paths only.** Tools accept only absolute host paths inside `allowed_roots`; sandbox paths (`/sessions/...`) are rejected with a clear hint.
- **codex is read-only.** `codex_review` never passes any write/auto-apply flag.
- License: MIT. No CONTRIBUTING.md in v1.
- Target OS: macOS/Linux (process-group kill uses unix syscalls).

---

### Task 1: Project scaffold + config package

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `.gitignore`
- Create: `LICENSE` (MIT)
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `config.Config` struct with fields: `AllowedRoots []string`, `AllowedCommands []string`, `Timeout time.Duration`, `MaxOutputBytes int`, `CodexExecArgs []string`.
  - `func Load(path string) (*Config, error)` — reads YAML, applies defaults, validates.
  - `func (c *Config) CommandAllowed(name string) bool`
  - `func (c *Config) ResolveAllowedDir(dir string) (string, error)` — returns the cleaned absolute path if it is inside an allowed root (after symlink resolution), else an error. Rejects `/sessions/` paths with a host-path hint.

- [ ] **Step 1: Initialize the module and dependencies**

Run:
```bash
cd /Users/jerryctt/code/personal/hostrunner-mcp
go mod init github.com/jerryctt/hostrunner-mcp
go get github.com/mark3labs/mcp-go@latest
go get gopkg.in/yaml.v3@latest
go get github.com/rs/zerolog@latest
```
Expected: `go.mod` and `go.sum` created with the three dependencies.

- [ ] **Step 2: Add `.gitignore` and `LICENSE`**

`.gitignore`:
```
/dist/
/hostrunner
*.test
*.out
.DS_Store
```

`LICENSE`: standard MIT text with `Copyright (c) 2026 Jerry`.

- [ ] **Step 3: Write the failing config test**

`internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultsAndValues(t *testing.T) {
	root := t.TempDir()
	p := writeTemp(t, "allowed_roots:\n  - "+root+"\nallowed_commands:\n  - codex\n  - git\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Timeout != 180*time.Second {
		t.Errorf("default timeout = %v, want 180s", c.Timeout)
	}
	if c.MaxOutputBytes != 200000 {
		t.Errorf("default max = %d, want 200000", c.MaxOutputBytes)
	}
	if !c.CommandAllowed("codex") || c.CommandAllowed("rm") {
		t.Errorf("CommandAllowed wrong")
	}
}

func TestResolveAllowedDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "proj")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	c := &Config{AllowedRoots: []string{root}}

	got, err := c.ResolveAllowedDir(sub)
	if err != nil {
		t.Fatalf("inside root should pass: %v", err)
	}
	if got == "" {
		t.Errorf("expected resolved path")
	}

	if _, err := c.ResolveAllowedDir("/etc"); err == nil {
		t.Errorf("outside root should fail")
	}
	if _, err := c.ResolveAllowedDir("/sessions/x/mnt/proj"); err == nil {
		t.Errorf("sandbox path should fail")
	}
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `go test ./internal/config/ -run Test -v`
Expected: FAIL — `Load`/`Config`/`ResolveAllowedDir` undefined.

- [ ] **Step 5: Implement the config package**

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AllowedRoots    []string      `yaml:"allowed_roots"`
	AllowedCommands []string      `yaml:"allowed_commands"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxOutputBytes  int           `yaml:"max_output_bytes"`
	CodexExecArgs   []string      `yaml:"codex_exec_args"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Timeout == 0 {
		c.Timeout = 180 * time.Second
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 200000
	}
	if len(c.CodexExecArgs) == 0 {
		c.CodexExecArgs = []string{"exec"}
	}
	if len(c.AllowedRoots) == 0 {
		return nil, fmt.Errorf("allowed_roots must not be empty")
	}
	for i, r := range c.AllowedRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, fmt.Errorf("allowed_root %q: %w", r, err)
		}
		c.AllowedRoots[i] = abs
	}
	return &c, nil
}

func (c *Config) CommandAllowed(name string) bool {
	for _, a := range c.AllowedCommands {
		if a == name {
			return true
		}
	}
	return false
}

func (c *Config) ResolveAllowedDir(dir string) (string, error) {
	if strings.HasPrefix(dir, "/sessions/") {
		return "", fmt.Errorf("got a sandbox path %q; pass the host path (e.g. /Users/...) instead", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve dir: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve dir: %w", err)
	}
	for _, root := range c.AllowedRoots {
		rootResolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if resolved == rootResolved || strings.HasPrefix(resolved, rootResolved+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q is not inside any allowed_root", dir)
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum .gitignore LICENSE internal/config/
git commit -m "feat: scaffold module and config package with allowlist validation"
```

---

### Task 2: exec package (argv executor)

**Files:**
- Create: `internal/exec/exec.go`
- Test: `internal/exec/exec_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `exec.Request{ Command string; Args []string; Dir string; Stdin string; Timeout time.Duration; MaxOutputBytes int }`
  - `exec.Result{ Stdout string; Stderr string; ExitCode int; Elapsed time.Duration; TimedOut bool; Truncated bool }`
  - `func Run(ctx context.Context, req Request) (Result, error)` — runs argv with no shell; feeds `Stdin`; enforces `Timeout` by killing the process group; truncates each stream to `MaxOutputBytes`.

- [ ] **Step 1: Write the failing test**

`internal/exec/exec_test.go`:
```go
package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesStdoutAndExit(t *testing.T) {
	r, err := Run(context.Background(), Request{
		Command: "printf", Args: []string{"hello"}, MaxOutputBytes: 1000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Stdout != "hello" || r.ExitCode != 0 {
		t.Errorf("got stdout=%q exit=%d", r.Stdout, r.ExitCode)
	}
}

func TestRunFeedsStdin(t *testing.T) {
	r, err := Run(context.Background(), Request{
		Command: "cat", Stdin: "piped-input", MaxOutputBytes: 1000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Stdout != "piped-input" {
		t.Errorf("stdin not forwarded, got %q", r.Stdout)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	r, err := Run(context.Background(), Request{
		Command: "sh", Args: []string{"-c", "exit 3"}, MaxOutputBytes: 1000,
	})
	if err != nil {
		t.Fatalf("Run should not error on non-zero exit: %v", err)
	}
	if r.ExitCode != 3 {
		t.Errorf("exit = %d, want 3", r.ExitCode)
	}
}

func TestRunTimeoutKills(t *testing.T) {
	start := time.Now()
	r, err := Run(context.Background(), Request{
		Command: "sleep", Args: []string{"5"}, Timeout: 150 * time.Millisecond, MaxOutputBytes: 1000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.TimedOut {
		t.Errorf("expected TimedOut")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("did not kill promptly")
	}
}

func TestRunTruncates(t *testing.T) {
	r, err := Run(context.Background(), Request{
		Command: "printf", Args: []string{"abcdefghij"}, MaxOutputBytes: 4,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.Truncated || len(r.Stdout) != 4 || !strings.HasPrefix("abcdefghij", r.Stdout) {
		t.Errorf("got %q truncated=%v", r.Stdout, r.Truncated)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/exec/ -v`
Expected: FAIL — `Run`/`Request`/`Result` undefined.

- [ ] **Step 3: Implement the executor**

`internal/exec/exec.go`:
```go
package exec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type Request struct {
	Command        string
	Args           []string
	Dir            string
	Stdin          string
	Timeout        time.Duration
	MaxOutputBytes int
}

type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Elapsed   time.Duration
	TimedOut  bool
	Truncated bool
}

func Run(ctx context.Context, req Request) (Result, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.Command(req.Command, req.Args...)
	cmd.Dir = req.Dir
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var res Result
	select {
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		res.TimedOut = true
	case err := <-done:
		if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				return Result{}, err
			}
		}
	}
	res.Elapsed = time.Since(start)
	res.ExitCode = cmd.ProcessState.ExitCode()

	res.Stdout, res.Truncated = truncate(out.String(), req.MaxOutputBytes)
	var st bool
	res.Stderr, st = truncate(errb.String(), req.MaxOutputBytes)
	res.Truncated = res.Truncated || st
	return res, nil
}

func truncate(s string, max int) (string, bool) {
	if max > 0 && len(s) > max {
		return s[:max], true
	}
	return s, false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/exec/ -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Commit**

```bash
git add internal/exec/
git commit -m "feat: add argv executor with timeout, stdin, and output truncation"
```

---

### Task 3: codex package (review logic)

**Files:**
- Create: `internal/codex/codex.go`
- Test: `internal/codex/codex_test.go`

**Interfaces:**
- Consumes: `exec.Request`, `exec.Result` from Task 2.
- Produces:
  - `codex.Runner` interface: `Run(ctx context.Context, req exec.Request) (exec.Result, error)` (satisfied by a thin wrapper over `exec.Run`).
  - `codex.ReviewParams{ Folder string; Scope string; Base string }`
  - `codex.ReviewResult{ Scope string; FilesTouched []string; Diff string; CodexOutput string; ExitCode int; Elapsed time.Duration; Truncated bool }`
  - `func Review(ctx context.Context, r Runner, codexCmd string, codexArgs []string, timeout time.Duration, maxBytes int, p ReviewParams) (ReviewResult, error)`
  - Exported helpers `DiffArgs(scope, base string) ([]string, error)` and `NameOnlyArgs(scope, base string) ([]string, error)`.

- [ ] **Step 1: Write the failing test**

`internal/codex/codex_test.go`:
```go
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

func TestDiffArgs(t *testing.T) {
	cases := map[string][]string{
		"working-tree": {"diff", "HEAD"},
		"staged":       {"diff", "--cached"},
	}
	for scope, want := range cases {
		got, err := DiffArgs(scope, "")
		if err != nil || strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("scope %s: got %v err %v", scope, got, err)
		}
	}
	got, err := DiffArgs("branch", "main")
	if err != nil || strings.Join(got, " ") != "diff main...HEAD" {
		t.Errorf("branch: got %v err %v", got, err)
	}
	if _, err := DiffArgs("branch", ""); err == nil {
		t.Errorf("branch without base should error")
	}
}

func TestReviewHappyPath(t *testing.T) {
	f := &fakeRunner{reply: map[string]exec.Result{
		"git rev-parse --is-inside-work-tree": {Stdout: "true\n"},
		"git diff HEAD":                       {Stdout: "diff body"},
		"git diff --name-only HEAD":           {Stdout: "a.go\nb.go\n"},
		"codex exec":                          {Stdout: "LGTM", ExitCode: 0},
	}}
	res, err := Review(context.Background(), f, "codex", []string{"exec"}, 0, 1000, ReviewParams{
		Folder: "/repo", Scope: "working-tree",
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.CodexOutput != "LGTM" || res.Diff != "diff body" {
		t.Errorf("unexpected result %+v", res)
	}
	if len(res.FilesTouched) != 2 {
		t.Errorf("files = %v", res.FilesTouched)
	}
	last := f.calls[len(f.calls)-1]
	if last.Command != "codex" || !strings.Contains(last.Stdin, "diff body") {
		t.Errorf("codex not invoked with diff in stdin: %+v", last)
	}
	for _, a := range last.Args {
		if a == "--write" || a == "--full-auto" {
			t.Errorf("codex must be read-only, got arg %q", a)
		}
	}
}

func TestReviewNotARepo(t *testing.T) {
	f := &fakeRunner{reply: map[string]exec.Result{
		"git rev-parse --is-inside-work-tree": {Stdout: "", ExitCode: 128},
	}}
	_, err := Review(context.Background(), f, "codex", []string{"exec"}, 0, 1000, ReviewParams{
		Folder: "/repo", Scope: "working-tree",
	})
	if err == nil {
		t.Errorf("expected not-a-repo error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/codex/ -v`
Expected: FAIL — `Review`/`DiffArgs` undefined.

- [ ] **Step 3: Implement the codex package**

`internal/codex/codex.go`:
```go
package codex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jerryctt/hostrunner-mcp/internal/exec"
)

type Runner interface {
	Run(ctx context.Context, req exec.Request) (exec.Result, error)
}

type ReviewParams struct {
	Folder string
	Scope  string
	Base   string
}

type ReviewResult struct {
	Scope        string
	FilesTouched []string
	Diff         string
	CodexOutput  string
	ExitCode     int
	Elapsed      time.Duration
	Truncated    bool
}

func DiffArgs(scope, base string) ([]string, error) {
	switch scope {
	case "", "working-tree":
		return []string{"diff", "HEAD"}, nil
	case "staged":
		return []string{"diff", "--cached"}, nil
	case "branch":
		if base == "" {
			return nil, fmt.Errorf("scope=branch requires a base ref")
		}
		return []string{"diff", base + "...HEAD"}, nil
	default:
		return nil, fmt.Errorf("unknown scope %q", scope)
	}
}

func NameOnlyArgs(scope, base string) ([]string, error) {
	args, err := DiffArgs(scope, base)
	if err != nil {
		return nil, err
	}
	out := []string{args[0], "--name-only"}
	return append(out, args[1:]...), nil
}

func buildPrompt(diff string) string {
	return "You are a senior engineer doing a focused code review. " +
		"Review only the following diff for correctness, bugs, security issues, " +
		"and clear design problems. Be specific and concise.\n\n" +
		"=== DIFF ===\n" + diff + "\n=== END DIFF ===\n"
}

func Review(ctx context.Context, r Runner, codexCmd string, codexArgs []string, timeout time.Duration, maxBytes int, p ReviewParams) (ReviewResult, error) {
	probe, err := r.Run(ctx, exec.Request{
		Command: "git", Args: []string{"rev-parse", "--is-inside-work-tree"},
		Dir: p.Folder, Timeout: timeout, MaxOutputBytes: maxBytes,
	})
	if err != nil {
		return ReviewResult{}, err
	}
	if probe.ExitCode != 0 || !strings.HasPrefix(probe.Stdout, "true") {
		return ReviewResult{}, fmt.Errorf("%s is not a git repository", p.Folder)
	}

	dArgs, err := DiffArgs(p.Scope, p.Base)
	if err != nil {
		return ReviewResult{}, err
	}
	diffRes, err := r.Run(ctx, exec.Request{
		Command: "git", Args: dArgs, Dir: p.Folder, Timeout: timeout, MaxOutputBytes: maxBytes,
	})
	if err != nil {
		return ReviewResult{}, err
	}

	nArgs, _ := NameOnlyArgs(p.Scope, p.Base)
	nameRes, err := r.Run(ctx, exec.Request{
		Command: "git", Args: nArgs, Dir: p.Folder, Timeout: timeout, MaxOutputBytes: maxBytes,
	})
	if err != nil {
		return ReviewResult{}, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(nameRes.Stdout), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}

	if strings.TrimSpace(diffRes.Stdout) == "" {
		return ReviewResult{Scope: scopeLabel(p.Scope), FilesTouched: files, Diff: "", CodexOutput: "No changes to review for this scope."}, nil
	}

	codexRes, err := r.Run(ctx, exec.Request{
		Command: codexCmd, Args: codexArgs, Dir: p.Folder,
		Stdin: buildPrompt(diffRes.Stdout), Timeout: timeout, MaxOutputBytes: maxBytes,
	})
	if err != nil {
		return ReviewResult{}, err
	}

	return ReviewResult{
		Scope:        scopeLabel(p.Scope),
		FilesTouched: files,
		Diff:         diffRes.Stdout,
		CodexOutput:  codexRes.Stdout,
		ExitCode:     codexRes.ExitCode,
		Elapsed:      codexRes.Elapsed,
		Truncated:    diffRes.Truncated || codexRes.Truncated,
	}, nil
}

func scopeLabel(s string) string {
	if s == "" {
		return "working-tree"
	}
	return s
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/codex/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codex/
git commit -m "feat: add codex review logic with scope-based diff and read-only invocation"
```

---

### Task 4: server package (tool wiring + audit)

**Files:**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `config.Config`, `codex.Runner`/`codex.Review`, `exec.Request`/`exec.Result`.
- Produces:
  - `func New(cfg *config.Config, r codex.Runner, logger zerolog.Logger) *mcpserver.MCPServer` — builds the MCP server with both tools registered.
  - Pure, testable handlers:
    - `func HandleCodexReview(ctx context.Context, cfg *config.Config, r codex.Runner, log zerolog.Logger, folder, scope, base string) (string, bool)` — returns (text, isError).
    - `func HandleRunCommand(ctx context.Context, cfg *config.Config, r codex.Runner, log zerolog.Logger, command string, args []string, folder string) (string, bool)`

- [ ] **Step 1: Write the failing test**

`internal/server/server_test.go`:
```go
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
		CodexExecArgs:   []string{"exec"},
		MaxOutputBytes:  1000,
	}, repo
}

func TestHandleCodexReviewRejectsOutsideRoot(t *testing.T) {
	cfg, _ := newCfg(t)
	r := &fakeRunner{}
	out, isErr := HandleCodexReview(context.Background(), cfg, r, zerolog.Nop(), "/etc", "working-tree", "")
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -v`
Expected: FAIL — handlers undefined.

- [ ] **Step 3: Implement the server package**

`internal/server/server.go`:
```go
package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/jerryctt/hostrunner-mcp/internal/codex"
	"github.com/jerryctt/hostrunner-mcp/internal/config"
	"github.com/jerryctt/hostrunner-mcp/internal/exec"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

func New(cfg *config.Config, r codex.Runner, log zerolog.Logger) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer("hostrunner", "0.1.0", mcpserver.WithRecovery())

	reviewTool := mcp.NewTool("codex_review",
		mcp.WithDescription("Run a read-only codex review of changes in a host project folder."),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Absolute HOST path to the git repo (e.g. /Users/you/proj). Not a sandbox path.")),
		mcp.WithString("scope", mcp.Description("working-tree (default), staged, or branch"), mcp.Enum("working-tree", "staged", "branch")),
		mcp.WithString("base", mcp.Description("Base ref, required when scope=branch")),
	)
	s.AddTool(reviewTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		folder, err := req.RequireString("folder")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, isErr := HandleCodexReview(ctx, cfg, r, log, folder, req.GetString("scope", "working-tree"), req.GetString("base", ""))
		if isErr {
			return mcp.NewToolResultError(out), nil
		}
		return mcp.NewToolResultText(out), nil
	})

	runTool := mcp.NewTool("run_command",
		mcp.WithDescription("Run an allowlisted command (argv, no shell) in a host folder."),
		mcp.WithString("command", mcp.Required(), mcp.Description("Executable name; must be in the server allowlist")),
		mcp.WithArray("args", mcp.WithStringItems(), mcp.Description("Arguments as a string array")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Absolute HOST path inside an allowed root")),
	)
	s.AddTool(runTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		command, err := req.RequireString("command")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		folder, err := req.RequireString("folder")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, isErr := HandleRunCommand(ctx, cfg, r, log, command, req.GetStringSlice("args", nil), folder)
		if isErr {
			return mcp.NewToolResultError(out), nil
		}
		return mcp.NewToolResultText(out), nil
	})

	return s
}

func HandleCodexReview(ctx context.Context, cfg *config.Config, r codex.Runner, log zerolog.Logger, folder, scope, base string) (string, bool) {
	dir, err := cfg.ResolveAllowedDir(folder)
	if err != nil {
		return err.Error(), true
	}
	res, err := codex.Review(ctx, r, "codex", cfg.CodexExecArgs, cfg.Timeout, cfg.MaxOutputBytes, codex.ReviewParams{
		Folder: dir, Scope: scope, Base: base,
	})
	log.Info().Str("tool", "codex_review").Str("folder", dir).Str("scope", scope).Err(err).Msg("invocation")
	if err != nil {
		return err.Error(), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Scope: %s\nFiles touched: %s\n\n--- Codex review ---\n%s\n", res.Scope, strings.Join(res.FilesTouched, ", "), res.CodexOutput)
	if res.Truncated {
		b.WriteString("\n[output truncated]\n")
	}
	return b.String(), false
}

func HandleRunCommand(ctx context.Context, cfg *config.Config, r codex.Runner, log zerolog.Logger, command string, args []string, folder string) (string, bool) {
	if !cfg.CommandAllowed(command) {
		return fmt.Sprintf("command %q is not allowed", command), true
	}
	dir, err := cfg.ResolveAllowedDir(folder)
	if err != nil {
		return err.Error(), true
	}
	res, err := r.Run(ctx, exec.Request{
		Command: command, Args: args, Dir: dir, Timeout: cfg.Timeout, MaxOutputBytes: cfg.MaxOutputBytes,
	})
	log.Info().Str("tool", "run_command").Str("command", command).Str("folder", dir).Int("exit", res.ExitCode).Err(err).Msg("invocation")
	if err != nil {
		return err.Error(), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "exit=%d elapsed=%s\n--- stdout ---\n%s\n--- stderr ---\n%s\n", res.ExitCode, res.Elapsed, res.Stdout, res.Stderr)
	if res.TimedOut {
		b.WriteString("[timed out]\n")
	}
	return b.String(), false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/server/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat: wire codex_review and run_command MCP tools with audit logging"
```

---

### Task 5: main entrypoint + examples + Makefile

**Files:**
- Create: `cmd/hostrunner/main.go`
- Create: `internal/exec/runner.go` (adapter implementing `codex.Runner`)
- Create: `examples/config.example.yaml`
- Create: `examples/claude_desktop_config.example.json`
- Create: `Makefile`

**Interfaces:**
- Consumes: `config.Load`, `server.New`, `exec.Run`.
- Produces: a runnable `hostrunner` binary; `exec.Runner{}` type implementing `codex.Runner`.

- [ ] **Step 1: Write the failing test for the runner adapter**

`internal/exec/runner_test.go`:
```go
package exec

import (
	"context"
	"testing"
)

func TestRunnerAdapter(t *testing.T) {
	var r Runner
	res, err := r.Run(context.Background(), Request{Command: "printf", Args: []string{"ok"}, MaxOutputBytes: 100})
	if err != nil || res.Stdout != "ok" {
		t.Errorf("adapter failed: %q %v", res.Stdout, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/exec/ -run TestRunnerAdapter -v`
Expected: FAIL — `Runner` type undefined.

- [ ] **Step 3: Implement the runner adapter**

`internal/exec/runner.go`:
```go
package exec

import "context"

// Runner adapts the package-level Run into a value that satisfies codex.Runner.
type Runner struct{}

func (Runner) Run(ctx context.Context, req Request) (Result, error) {
	return Run(ctx, req)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/exec/ -v`
Expected: PASS.

- [ ] **Step 5: Write `cmd/hostrunner/main.go`**

```go
package main

import (
	"flag"
	"os"

	"github.com/jerryctt/hostrunner-mcp/internal/config"
	"github.com/jerryctt/hostrunner-mcp/internal/exec"
	"github.com/jerryctt/hostrunner-mcp/internal/server"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

func main() {
	cfgPath := flag.String("config", os.Getenv("HOSTRUNNER_CONFIG"), "path to config.yaml")
	flag.Parse()

	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	if *cfgPath == "" {
		log.Fatal().Msg("missing -config (or HOSTRUNNER_CONFIG)")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	s := server.New(cfg, exec.Runner{}, log)
	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatal().Err(err).Msg("server exited")
	}
}
```

Note: the two `server` imports collide — alias the internal one. Use:
```go
	internalserver "github.com/jerryctt/hostrunner-mcp/internal/server"
	mcpserver "github.com/mark3labs/mcp-go/server"
```
and call `internalserver.New(...)` and `mcpserver.ServeStdio(...)`.

- [ ] **Step 6: Write the example config and registration files**

`examples/config.example.yaml`:
```yaml
allowed_roots:
  - /Users/jerryctt/code
allowed_commands:
  - codex
  - git
timeout: 180s
max_output_bytes: 200000
# codex_exec_args: ["exec"]   # adjust to your codex CLI's non-interactive review flags
```

`examples/claude_desktop_config.example.json`:
```json
{
  "mcpServers": {
    "hostrunner": {
      "command": "/usr/local/bin/hostrunner",
      "args": ["-config", "/Users/jerryctt/.config/hostrunner/config.yaml"]
    }
  }
}
```

- [ ] **Step 7: Write the Makefile**

```makefile
.PHONY: build test vet
build:
	go build -o hostrunner ./cmd/hostrunner
test:
	go test ./...
vet:
	go vet ./...
```

- [ ] **Step 8: Verify build and full test suite**

Run:
```bash
go vet ./... && go build -o hostrunner ./cmd/hostrunner && go test ./...
```
Expected: builds cleanly; all tests PASS.

- [ ] **Step 9: Commit**

```bash
git add cmd/ internal/exec/runner.go internal/exec/runner_test.go examples/ Makefile
git commit -m "feat: add main entrypoint, runner adapter, examples, and Makefile"
```

---

### Task 6: Public-repo packaging & docs

**Files:**
- Create: `README.md` (English, primary)
- Create: `README_zh.md` (Traditional Chinese)
- Create: `skills/codex-loop/SKILL.md`
- Create: `.goreleaser.yaml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

**Interfaces:** none (docs/infra). Verification is build + workflow lint.

- [ ] **Step 1: Write `README.md`**

Follow the section outline from the spec §11: language switcher line (`English | [繁體中文](README_zh.md)`), badges, Purpose/Why, How it works (ASCII diagram from spec §3), Features, Requirements (host codex installed+authed; git; Go only to build), Installation (`go install ./cmd/hostrunner` / download release binary / build from source), Configuration (the `config.example.yaml` fields), Register with Claude Desktop (the `mcpServers` snippet), Available tools (`codex_review` params `folder`/`scope`/`base`; `run_command` params `command`/`args`/`folder`; return shapes), Installing the Cowork skill (copy `skills/codex-loop/` into the skills directory), Usage (a worked edit→review→edit example), Security (allowlist, no shell, native host process, not Docker, host-path rule), Development (`make build` / `make test`), License (MIT).

- [ ] **Step 2: Write `README_zh.md`**

Mirror every README.md section in Traditional Chinese; top line `[English](README.md) | 繁體中文`.

- [ ] **Step 3: Write `skills/codex-loop/SKILL.md`**

A Cowork skill with YAML frontmatter (`name: codex-loop`, `description:` covering "review my changes with codex", "edit-review loop"). Body instructs: after editing files, call the `codex_review` MCP tool with the connected folder's HOST path and `scope: working-tree`; summarize findings; iterate. Explicitly warns to pass the host path (the path Read/Write use), never a `/sessions/...` path.

- [ ] **Step 4: Write `.goreleaser.yaml`**

Builds for `darwin`/`linux` × `amd64`/`arm64` from `./cmd/hostrunner`, binary name `hostrunner`, archives as tar.gz.

- [ ] **Step 5: Write CI and release workflows**

`.github/workflows/ci.yml`: on push/PR, `go vet ./...` + `go test ./...` on the Go version from `go.mod`.
`.github/workflows/release.yml`: on tag `v*`, run goreleaser to publish binaries.

- [ ] **Step 6: Verify**

Run:
```bash
go build ./... && go test ./...
```
Expected: clean build, all tests PASS. (If `goreleaser` is installed locally, also run `goreleaser check`.)

- [ ] **Step 7: Commit**

```bash
git add README.md README_zh.md skills/ .goreleaser.yaml .github/
git commit -m "docs: add README (en/zh), codex-loop skill, and release/CI config"
```

---

## Self-Review

**Spec coverage:**
- Purpose / bridge / loop → Tasks 3–6 (codex_review + skill). ✓
- stdio MCP server, mcp-go → Task 4 (`server.New`), Task 5 (`ServeStdio`). ✓
- Generic `run_command`, narrow/allowlisted → Task 4. ✓
- Components config/exec/server/codex → Tasks 1–4 (one package each). ✓
- Host-path rule + sandbox-path rejection → Task 1 (`ResolveAllowedDir`), surfaced in tool descriptions Task 4. ✓
- codex read-only → Task 3 (no write flags; test asserts it). ✓
- Config (allowed_roots/commands/timeout/max_output_bytes) → Task 1 + example Task 5. ✓
- Claude Desktop registration → Task 5 example + Task 6 README. ✓
- Error handling (not-a-repo, command/dir not allowed, timeout, truncation) → Tasks 1–4 with tests. ✓
- Audit log → Task 4 (zerolog). ✓
- No Docker / native process → Task 5 main + Task 6 README security. ✓
- Testing strategy (unit config/exec/codex, integration via fake runner, manual real codex) → Tasks 1–4 tests; manual smoke noted in Task 5 step 8 / README usage. ✓
- Repo layout + README en/zh + skill + goreleaser + CI → Task 6. ✓

**Placeholder scan:** No "TBD/TODO/handle edge cases" left; the one genuine variability — exact codex non-interactive flags — is surfaced as the configurable `codex_exec_args` with a documented default, not a placeholder.

**Type consistency:** `exec.Request`/`exec.Result` used identically across Tasks 2–5; `codex.Runner` (Task 3) is implemented by `exec.Runner` (Task 5) and injected in Task 4; `ReviewParams`/`ReviewResult` field names match between Task 3 definition and Task 4 use; handler signatures in Task 4 interfaces match their implementations.

## Open Assumptions (validate during implementation)

1. Claude Desktop on this machine supports a custom local stdio MCP server. If not, fall back to a file-bridge daemon.
2. Cowork exposes the connected folder's host path for passing to the tools.
3. The installed codex CLI's non-interactive review invocation matches `codex exec` reading the prompt from stdin; if not, adjust `codex_exec_args` (and, if needed, the stdin-vs-arg handling in `internal/codex`).
