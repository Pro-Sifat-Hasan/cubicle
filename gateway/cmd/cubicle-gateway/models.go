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

// listModels runs `cubicle-agents models --json`, which asks the provider's
// official SDK for the models the configured key can use. The worker prints a
// single JSON object on its last stdout line: {"models": [...]} or {"error": "..."}.
func listModels(ctx context.Context, command string, args, env []string) ([]string, error) {
	out, err := supervisor.Capture(ctx, command, append(append([]string{}, args...), "models", "--json"), env)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || len(out) == 0 {
			return nil, fmt.Errorf("could not run the agent worker: %w", err)
		}
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	var res struct {
		Models []string `json:"models"`
		Error  string   `json:"error"`
	}
	if err := json.Unmarshal(lines[len(lines)-1], &res); err != nil {
		return nil, errors.New("the agent worker returned an unexpected response")
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return res.Models, nil
}
