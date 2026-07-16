package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// keepFinished is how long a finished job's result stays retrievable.
const keepFinished = 30 * time.Minute

// reviewJob is a background codex review. The output/isErr/ended fields are
// written exactly once by finish() before done is closed; readers must only
// access them after observing <-done (the channel close provides the
// happens-before edge).
type reviewJob struct {
	ID      string
	Folder  string
	Root    string // worktree root used as the duplicate-detection key
	Mode    string
	Started time.Time

	done   chan struct{}
	output string
	isErr  bool
	ended  time.Time
}

func (j *reviewJob) finish(output string, isErr bool) {
	j.output = output
	j.isErr = isErr
	j.ended = time.Now()
	close(j.done)
}

func (j *reviewJob) finished() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

type jobStore struct {
	mu   sync.Mutex
	jobs map[string]*reviewJob

	// ctx is the server-lifetime context background jobs run under, so that
	// shutdown() can cancel in-flight codex processes instead of orphaning
	// them (the executor kills the whole process group on cancellation).
	ctx    context.Context
	cancel context.CancelFunc
}

func newJobStore() *jobStore {
	ctx, cancel := context.WithCancel(context.Background())
	return &jobStore{jobs: make(map[string]*reviewJob), ctx: ctx, cancel: cancel}
}

// shutdown cancels all running jobs and waits up to grace for their
// goroutines to observe the cancellation and kill their codex processes.
func (s *jobStore) shutdown(grace time.Duration) {
	s.cancel()
	deadline := time.After(grace)
	s.mu.Lock()
	var pending []*reviewJob
	for _, j := range s.jobs {
		if !j.finished() {
			pending = append(pending, j)
		}
	}
	s.mu.Unlock()
	for _, j := range pending {
		select {
		case <-j.done:
		case <-deadline:
			return
		}
	}
}

// add registers a new job. It refuses to start a second review for a folder
// that already has one running: each review spawns a long-running codex
// process on the host, so retries or impatient clients must poll the existing
// job instead of stacking processes.
func (s *jobStore) add(folder, mode string) (*reviewJob, error) {
	root := worktreeRoot(folder)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	for _, j := range s.jobs {
		if j.Root == root && !j.finished() {
			return nil, fmt.Errorf(
				"a review of %s is already running (job %s, started %s ago); poll codex_review_status with that job_id instead of starting another",
				j.Folder, j.ID, time.Since(j.Started).Round(time.Second),
			)
		}
	}
	j := &reviewJob{
		ID:      newJobID(),
		Folder:  folder,
		Root:    root,
		Mode:    mode,
		Started: time.Now(),
		done:    make(chan struct{}),
	}
	s.jobs[j.ID] = j
	return j, nil
}

func (s *jobStore) get(id string) (*reviewJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked() // enforce keepFinished on retrieval, not only on add
	j, ok := s.jobs[id]
	return j, ok
}

// gcLocked drops jobs that finished more than keepFinished ago.
// Callers must hold s.mu.
func (s *jobStore) gcLocked() {
	now := time.Now()
	for id, j := range s.jobs {
		if j.finished() && now.Sub(j.ended) > keepFinished {
			delete(s.jobs, id)
		}
	}
}

// worktreeRoot returns the nearest ancestor of dir (including dir itself)
// containing a .git entry — the same worktree codex will discover from any
// directory inside it, so it serves as the duplicate-detection key for jobs
// started from different subdirectories (e.g. /repo/internal and /repo/cmd).
// The .git entry may be a directory or a file (linked worktrees, submodules);
// os.Stat accepts both. Falls back to dir when no .git is found. This is a
// pure filesystem check — the server still never runs git itself. dir is
// already absolute and symlink-resolved by config.ResolveAllowedDir.
func worktreeRoot(dir string) string {
	p := dir
	for {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return dir
		}
		p = parent
	}
}

func newJobID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively unrecoverable; fall back to time.
		return hex.EncodeToString([]byte(time.Now().Format("150405.000")))
	}
	return hex.EncodeToString(b)
}
