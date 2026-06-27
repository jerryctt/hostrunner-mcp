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

func TestStreamOutputDefaultTrue(t *testing.T) {
	root := t.TempDir()
	p := writeTemp(t, "allowed_roots:\n  - "+root+"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.StreamEnabled() {
		t.Errorf("StreamEnabled() = false, want true (default on)")
	}
}

func TestStreamOutputExplicitFalse(t *testing.T) {
	root := t.TempDir()
	p := writeTemp(t, "allowed_roots:\n  - "+root+"\nstream_output: false\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.StreamEnabled() {
		t.Errorf("StreamEnabled() = true, want false when stream_output: false")
	}
}
