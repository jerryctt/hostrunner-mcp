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
