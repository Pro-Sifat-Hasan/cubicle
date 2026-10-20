//go:build !windows

// Package winsvc is a no-op outside Windows; use systemd or Docker there
// (see deploy/).
package winsvc

import (
	"context"
	"errors"
)

const Name = "CubicleGateway"

var errUnsupported = errors.New("Windows services are only available on Windows; use deploy/cubicle.service or Docker")

func IsService() bool                                  { return false }
func RunService(func(ctx context.Context) error) error { return errUnsupported }
func Install() error                                   { return errUnsupported }
func Uninstall() error                                 { return errUnsupported }
func Start() error                                     { return errUnsupported }
func Stop() error                                      { return errUnsupported }
