//go:build !windows

package main

import (
	"context"
	"errors"

	"github.com/screwys/igloo/internal/windowsupdate"
)

type unsupportedLifecycle struct{}

func newPlatformLifecycle() windowsupdate.Lifecycle { return unsupportedLifecycle{} }
func (unsupportedLifecycle) WaitForProcess(context.Context, int) error {
	return errors.New("Windows only")
}
func (unsupportedLifecycle) Start(context.Context, windowsupdate.ApplyPlan) error {
	return errors.New("Windows only")
}
func (unsupportedLifecycle) Stop(context.Context, windowsupdate.ApplyPlan) error {
	return errors.New("Windows only")
}
func (unsupportedLifecycle) WaitHealthy(context.Context, windowsupdate.ApplyPlan) error {
	return errors.New("Windows only")
}
