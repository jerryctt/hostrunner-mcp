package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jerryctt/hostrunner-mcp/internal/codex"
	"github.com/jerryctt/hostrunner-mcp/internal/config"
	"github.com/jerryctt/hostrunner-mcp/internal/exec"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// statusLongPoll is how long codex_review_status blocks waiting for the job
// to finish before reporting "running". It must stay well under the MCP
// client's per-call timeout (Claude Desktop kills tool calls at ~180s).
const statusLongPoll = 50 * time.Second

// New builds the MCP server. The returned shutdown func cancels any
// still-running background review jobs (killing their codex process groups)
// and should be called after the server stops serving.
func New(cfg *config.Config, r codex.Runner, log zerolog.Logger) (*mcpserver.MCPServer, func()) {
	s := mcpserver.NewMCPServer("hostrunner", "0.2.1", mcpserver.WithRecovery())
	jobs := newJobStore()

	startTool := mcp.NewTool("codex_review_start",
		mcp.WithDescription("Start a read-only codex review of changes in a host project folder. The review runs in the background on the host and this call returns a job_id immediately; poll codex_review_status to get the result. (Reviews often take several minutes — longer than the MCP client's ~180s per-call limit — which is why they run as background jobs.) Uses 'codex review', which computes the diff itself and does not modify files."),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Absolute HOST path to the git repo (e.g. /Users/you/proj). Not a sandbox path.")),
		mcp.WithString("scope",
			mcp.Description("Which changes to review: uncommitted (default, staged+unstaged+untracked), base (against a base branch, requires base param), or commit (a specific commit, requires commit param)"),
			mcp.Enum("uncommitted", "base", "commit"),
		),
		mcp.WithString("base", mcp.Description("Base branch or ref, required when scope=base (e.g. main)")),
		mcp.WithString("commit", mcp.Description("Commit SHA, required when scope=commit")),
		mcp.WithString("prompt", mcp.Description("Custom review instructions. ONLY pass this when the user explicitly asks to narrow or focus the review (e.g. \"focus on concurrency\", \"only review the auth changes\"); omit it for a normal general review. Safe to combine with any scope: the server folds the scope into the prompt text, because the codex CLI rejects scope flags alongside a prompt.")),
	)
	s.AddTool(startTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		folder, err := req.RequireString("folder")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, isErr := HandleReviewStart(
			cfg, r, log, jobs,
			folder,
			req.GetString("scope", "uncommitted"),
			req.GetString("base", ""),
			req.GetString("commit", ""),
			req.GetString("prompt", ""),
		)
		if isErr {
			return mcp.NewToolResultError(out), nil
		}
		return mcp.NewToolResultText(out), nil
	})

	statusTool := mcp.NewTool("codex_review_status",
		mcp.WithDescription("Get the status/result of a background review started with codex_review_start. Blocks up to ~50s waiting for completion, then returns either the finished review or a 'running' notice — keep calling it with the same job_id until it completes. Finished results stay available for ~30 minutes."),
		mcp.WithString("job_id", mcp.Required(), mcp.Description("The job_id returned by codex_review_start")),
	)
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jobID, err := req.RequireString("job_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, isErr := HandleReviewStatus(ctx, jobs, jobID, statusLongPoll)
		if isErr {
			return mcp.NewToolResultError(out), nil
		}
		return mcp.NewToolResultText(out), nil
	})

	runTool := mcp.NewTool("run_command",
		mcp.WithDescription("Run an allowlisted command (argv, no shell) in a host folder."),
		mcp.WithString("command", mcp.Required(), mcp.Description("Executable name; must be in the server allowlist")),
		mcp.WithArray("args", mcp.WithStringItems(), mcp.Description("Arguments as a string array")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Absolute HOST path inside an allowed root (e.g. /Users/you/proj). Not a /sessions/... sandbox path.")),
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

	return s, func() { jobs.shutdown(3 * time.Second) }
}

// HandleReviewStart validates the request, then runs the review in a
// background goroutine so the MCP call returns immediately. The goroutine
// deliberately does not use the request context (it is cancelled as soon as
// this call returns); it runs under the job store's server-lifetime context
// so shutdown() can still cancel it.
func HandleReviewStart(cfg *config.Config, r codex.Runner, log zerolog.Logger, jobs *jobStore, folder, mode, base, commit, prompt string) (string, bool) {
	dir, err := cfg.ResolveAllowedDir(folder)
	if err != nil {
		return err.Error(), true
	}
	// Catch bad scope/base/commit combinations now, not minutes later.
	if _, _, err := codex.ReviewArgs(mode, base, commit, prompt); err != nil {
		return err.Error(), true
	}
	label := mode
	if label == "" {
		label = "uncommitted"
	}
	j, err := jobs.add(dir, label)
	if err != nil {
		return err.Error(), true
	}
	log.Info().Str("tool", "codex_review_start").Str("job", j.ID).Str("folder", dir).Str("mode", label).Msg("invocation")
	go func() {
		out, isErr := HandleCodexReview(jobs.ctx, cfg, r, log, dir, mode, base, commit, prompt)
		j.finish(out, isErr)
	}()
	return fmt.Sprintf(
		"Review started in the background.\njob_id: %s\nmode: %s\nfolder: %s\n\nCall codex_review_status with this job_id to get the result. Reviews typically take 1-10 minutes; the server-side limit is %s (config 'timeout').",
		j.ID, label, dir, cfg.Timeout,
	), false
}

// HandleReviewStatus reports on a background review, blocking up to wait for
// it to finish (long-poll) so callers need fewer round trips.
func HandleReviewStatus(ctx context.Context, jobs *jobStore, jobID string, wait time.Duration) (string, bool) {
	j, ok := jobs.get(jobID)
	if !ok {
		return fmt.Sprintf("unknown job_id %q (finished jobs are kept for ~%s; start a new review with codex_review_start)", jobID, keepFinished), true
	}
	select {
	case <-j.done:
		prefix := fmt.Sprintf("status: completed (job %s, elapsed %s)\n\n", j.ID, j.ended.Sub(j.Started).Round(time.Second))
		return prefix + j.output, j.isErr
	case <-ctx.Done():
		return "status check cancelled by client", true
	case <-time.After(wait):
		return fmt.Sprintf(
			"status: running (job %s, elapsed %s)\nThe review is still in progress. Call codex_review_status again with the same job_id.",
			j.ID, time.Since(j.Started).Round(time.Second),
		), false
	}
}

func HandleCodexReview(ctx context.Context, cfg *config.Config, r codex.Runner, log zerolog.Logger, folder, mode, base, commit, prompt string) (string, bool) {
	dir, err := cfg.ResolveAllowedDir(folder)
	if err != nil {
		return err.Error(), true
	}
	var streamTo io.Writer
	if cfg.StreamEnabled() {
		streamTo = os.Stderr
	}
	res, err := codex.Review(ctx, r, "codex", cfg.CodexExtraArgs, cfg.Timeout, cfg.MaxOutputBytes, streamTo, codex.ReviewParams{
		Folder: dir,
		Mode:   mode,
		Base:   base,
		Commit: commit,
		Prompt: prompt,
	})
	if err != nil {
		log.Info().Str("tool", "codex_review").Str("folder", dir).Str("mode", mode).Err(err).Msg("invocation")
		return err.Error(), true
	}
	log.Info().Str("tool", "codex_review").Str("folder", dir).Str("mode", mode).Dur("elapsed", res.Elapsed).Int("exit", res.ExitCode).Err(err).Msg("invocation")
	var b strings.Builder
	fmt.Fprintf(&b, "Mode: %s (codex exit %d)\n\n--- Codex review ---\n%s\n", res.Mode, res.ExitCode, res.Output)
	// codex's stderr is its verbose progress/exec trace; it is already streamed
	// live to the desktop log (stream_output). Only surface it in the result when
	// codex itself failed, as diagnostics.
	if res.ExitCode != 0 && res.Stderr != "" {
		fmt.Fprintf(&b, "\n--- codex stderr (exit %d) ---\n%s\n", res.ExitCode, res.Stderr)
	}
	if res.TimedOut {
		fmt.Fprintf(&b, "\n[codex was killed after the server-side timeout of %s (config 'timeout'); the review is incomplete]\n", cfg.Timeout)
	}
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
	var streamTo io.Writer
	if cfg.StreamEnabled() {
		streamTo = os.Stderr
	}
	res, err := r.Run(ctx, exec.Request{
		Command: command, Args: args, Dir: dir, Timeout: cfg.Timeout, MaxOutputBytes: cfg.MaxOutputBytes,
		StreamTo: streamTo,
	})
	if err != nil {
		log.Info().Str("tool", "run_command").Str("command", command).Str("folder", dir).Err(err).Msg("invocation")
		return err.Error(), true
	}
	log.Info().Str("tool", "run_command").Str("command", command).Str("folder", dir).Int("exit", res.ExitCode).Dur("elapsed", res.Elapsed).Err(err).Msg("invocation")
	var b strings.Builder
	fmt.Fprintf(&b, "exit=%d elapsed=%s\n--- stdout ---\n%s\n--- stderr ---\n%s\n", res.ExitCode, res.Elapsed, res.Stdout, res.Stderr)
	if res.TimedOut {
		b.WriteString("[timed out]\n")
	}
	return b.String(), false
}
