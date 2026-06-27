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
	// MaxOutputBytes caps combined stdout/stderr output; 0 means unlimited.
	MaxOutputBytes int
}

type Result struct {
	Stdout    string
	Stderr    string
	// ExitCode is the process exit code; it is -1 when TimedOut is true.
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
