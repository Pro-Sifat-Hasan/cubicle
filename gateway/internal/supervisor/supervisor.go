// Package supervisor keeps the Python agent worker running: it starts the
// process, streams its logs, and restarts it with exponential backoff if it
// crashes. Calling Restart (e.g. after an API key changes) bounces it.
package supervisor

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type Supervisor struct {
	Command string
	Args    []string
	// Env returns extra KEY=VALUE pairs, evaluated on every (re)start.
	Env func() []string
	// OnExit is called each time the worker stops.
	OnExit func(err error)
	Log    *slog.Logger

	restart chan struct{}
}

func New(command string, args []string, env func() []string, log *slog.Logger) *Supervisor {
	return &Supervisor{Command: command, Args: args, Env: env, Log: log, restart: make(chan struct{}, 1)}
}

// Discover finds the bundled worker next to the gateway executable.
func Discover() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	name := "cubicle-agents"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, p := range []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "cubicle-agents", name),
		filepath.Join(dir, "agents", name),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func (s *Supervisor) Restart() {
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

func (s *Supervisor) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := s.runOnce(ctx)
		if s.OnExit != nil {
			s.OnExit(err)
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		s.Log.Warn("agent worker stopped; restarting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		case <-s.restart:
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func (s *Supervisor) runOnce(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, s.Command, s.Args...)
	cmd.Env = append(os.Environ(), s.Env()...)
	cmd.WaitDelay = 5 * time.Second
	hideWindow(cmd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	s.Log.Info("agent worker started", "pid", cmd.Process.Pid, "command", s.Command)
	go s.pipe(stdout)
	go s.pipe(stderr)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-s.restart:
		s.Log.Info("restarting agent worker on request")
		cancel()
		<-done
		return nil
	}
}

// Capture runs the worker command once with extra arguments and returns its
// stdout. Used for short utility calls such as listing a provider's models.
func Capture(ctx context.Context, command string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	return cmd.Output()
}

func (s *Supervisor) pipe(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		s.Log.Info(sc.Text(), "src", "agents")
	}
	if err := sc.Err(); err != nil {
		s.Log.Warn("stopped reading agent worker output", "err", err)
	}
}
