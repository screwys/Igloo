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
	return errors.New("only supported on Windows")
}
func (unsupportedLifecycle) MigrateSQLite(context.Context, windowsupdate.ApplyPlan) (func() error, error) {
	return nil, errors.New("only supported on Windows")
}
func (unsupportedLifecycle) Start(context.Context, windowsupdate.ApplyPlan) error {
	return errors.New("only supported on Windows")
}
func (unsupportedLifecycle) Stop(context.Context, windowsupdate.ApplyPlan) error {
	return errors.New("only supported on Windows")
}
func (unsupportedLifecycle) WaitHealthy(context.Context, windowsupdate.ApplyPlan) error {
	return errors.New("only supported on Windows")
}
