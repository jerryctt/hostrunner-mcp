package server

import (
	"context"
	"fmt"
	"io"
	"os"
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
		mcp.WithDescription("Run a read-only codex review of changes in a host project folder. Uses 'codex review' which computes the diff itself and does not modify files."),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Absolute HOST path to the git repo (e.g. /Users/you/proj). Not a sandbox path.")),
		mcp.WithString("scope",
			mcp.Description("Which changes to review: uncommitted (default, staged+unstaged+untracked), base (against a base branch, requires base param), or commit (a specific commit, requires commit param)"),
			mcp.Enum("uncommitted", "base", "commit"),
		),
		mcp.WithString("base", mcp.Description("Base branch or ref, required when scope=base (e.g. main)")),
		mcp.WithString("commit", mcp.Description("Commit SHA, required when scope=commit")),
		mcp.WithString("prompt", mcp.Description("Optional custom review instructions passed to 'codex review' (e.g. \"focus on concurrency and error handling\", \"only review the auth changes\"). Leave empty for a general review.")),
	)
	s.AddTool(reviewTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		folder, err := req.RequireString("folder")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, isErr := HandleCodexReview(
			ctx, cfg, r, log,
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

	return s
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
