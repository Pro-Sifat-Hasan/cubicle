//go:build !windows

package supervisor

import "os/exec"

func hideWindow(*exec.Cmd) {}
