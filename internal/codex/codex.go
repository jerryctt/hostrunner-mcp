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
	TimedOut  bool
	Truncated bool
}

// ReviewArgs builds the codex CLI arguments for the given mode.
//
// In the codex CLI the scope flags are mutually exclusive with the positional
// [PROMPT] argument ("error: the argument '--uncommitted' cannot be used with
// '[PROMPT]'", same for --base and --commit). So:
//
//   - prompt == "": the scope is passed as a flag.
//     mode "" or "uncommitted" -> ["review", "--uncommitted"]
//     mode "base"              -> ["review", "--base", base]   (base must be non-empty)
//     mode "commit"            -> ["review", "--commit", sha]  (commit must be non-empty)
//   - prompt != "": no scope flag is emitted; the scope is folded into the
//     prompt text and returned as `positional`, to be appended as the last
//     CLI argument.
func ReviewArgs(mode, base, commit, prompt string) (args []string, positional string, err error) {
	var scope string
	switch mode {
	case "", "uncommitted":
		args = []string{"review", "--uncommitted"}
		scope = "Review the uncommitted changes (staged, unstaged, and untracked)."
	case "base":
		if base == "" {
			return nil, "", fmt.Errorf("mode=base requires a base ref")
		}
		args = []string{"review", "--base", base}
		scope = fmt.Sprintf("Review the changes against base branch %s.", base)
	case "commit":
		if commit == "" {
			return nil, "", fmt.Errorf("mode=commit requires a commit sha")
		}
		args = []string{"review", "--commit", commit}
		scope = fmt.Sprintf("Review the changes introduced by commit %s.", commit)
	default:
		return nil, "", fmt.Errorf("unknown mode %q", mode)
	}
	if prompt != "" {
		return []string{"review"}, scope + " " + prompt, nil
	}
	return args, "", nil
}

// Review invokes `codex review` with the appropriate mode flag in p.Folder.
// It does no git work itself; codex computes the diff.
// streamTo, if non-nil, receives live stdout+stderr in addition to the captured Result.
func Review(ctx context.Context, r Runner, codexCmd string, extraArgs []string, timeout time.Duration, maxBytes int, streamTo io.Writer, p ReviewParams) (ReviewResult, error) {
	args, positional, err := ReviewArgs(p.Mode, p.Base, p.Commit, p.Prompt)
	if err != nil {
		return ReviewResult{}, err
	}
	args = append(args, extraArgs...)
	if positional != "" {
		args = append(args, positional)
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
		TimedOut:  res.TimedOut,
		Truncated: res.Truncated,
	}, nil
}

func modeLabel(mode string) string {
	if mode == "" {
		return "uncommitted"
	}
	return mode
}
