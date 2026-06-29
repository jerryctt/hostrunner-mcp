package codex

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jerryctt/hostrunner-mcp/internal/exec"
)

type Runner interface {
	Run(ctx context.Context, req exec.Request) (exec.Result, error)
}

type ReviewParams struct {
	Folder string
	Mode   string
	Base   string
	Commit string
	// Prompt is optional custom review instructions, appended as the trailing
	// positional PROMPT arg to `codex review`. Leave empty for a general review.
	Prompt string
}

type ReviewResult struct {
	Mode      string
	Output    string
	Stderr    string
	ExitCode  int
	Elapsed   time.Duration
	Truncated bool
}

// ReviewArgs builds the codex CLI arguments for the given mode.
// mode "" or "uncommitted" -> ["review", "--uncommitted"]
// mode "base"              -> ["review", "--base", base]   (base must be non-empty)
// mode "commit"            -> ["review", "--commit", sha]  (commit must be non-empty)
func ReviewArgs(mode, base, commit string) ([]string, error) {
	switch mode {
	case "", "uncommitted":
		return []string{"review", "--uncommitted"}, nil
	case "base":
		if base == "" {
			return nil, fmt.Errorf("mode=base requires a base ref")
		}
		return []string{"review", "--base", base}, nil
	case "commit":
		if commit == "" {
			return nil, fmt.Errorf("mode=commit requires a commit sha")
		}
		return []string{"review", "--commit", commit}, nil
	default:
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
}

// Review invokes `codex review` with the appropriate mode flag in p.Folder.
// It does no git work itself; codex computes the diff.
// streamTo, if non-nil, receives live stdout+stderr in addition to the captured Result.
func Review(ctx context.Context, r Runner, codexCmd string, extraArgs []string, timeout time.Duration, maxBytes int, streamTo io.Writer, p ReviewParams) (ReviewResult, error) {
	args, err := ReviewArgs(p.Mode, p.Base, p.Commit)
	if err != nil {
		return ReviewResult{}, err
	}
	args = append(args, extraArgs...)
	if p.Prompt != "" {
		args = append(args, p.Prompt)
	}

	res, err := r.Run(ctx, exec.Request{
		Command:        codexCmd,
		Args:           args,
		Dir:            p.Folder,
		Timeout:        timeout,
		MaxOutputBytes: maxBytes,
		StreamTo:       streamTo,
	})
	if err != nil {
		return ReviewResult{}, err
	}

	return ReviewResult{
		Mode:      modeLabel(p.Mode),
		Output:    res.Stdout,
		Stderr:    res.Stderr,
		ExitCode:  res.ExitCode,
		Elapsed:   res.Elapsed,
		Truncated: res.Truncated,
	}, nil
}

func modeLabel(mode string) string {
	if mode == "" {
		return "uncommitted"
	}
	return mode
}
