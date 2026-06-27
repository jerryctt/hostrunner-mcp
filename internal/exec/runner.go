package exec

import "context"

// Runner adapts the package-level Run into a value that satisfies codex.Runner.
type Runner struct{}

func (Runner) Run(ctx context.Context, req Request) (Result, error) {
	return Run(ctx, req)
}
