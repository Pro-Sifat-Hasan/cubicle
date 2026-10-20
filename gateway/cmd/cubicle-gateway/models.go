package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/supervisor"
)

// runWorkerJSON runs a one-off worker command such as `cubicle-agents models --json`
// or `cubicle-agents mcp-tools --json`. The worker prints a single JSON object on
// its last stdout line, which is decoded into out. A non-zero exit is fine as
// long as that line is present: it carries the error message for the owner.
func runWorkerJSON(ctx context.Context, command string, baseArgs, args, env []string, out any) error {
	stdout, err := supervisor.Capture(ctx, command, append(append([]string{}, baseArgs...), args...), env)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || len(stdout) == 0 {
			return fmt.Errorf("could not run the agent worker: %w", err)
		}
	}
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	if err := json.Unmarshal(bytes.TrimSpace(lines[len(lines)-1]), out); err != nil {
		return errors.New("the agent worker returned an unexpected response")
	}
	return nil
}
