package windowsupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Lifecycle interface {
	WaitForProcess(context.Context, int) error
	Start(context.Context, ApplyPlan) error
	Stop(context.Context, ApplyPlan) error
	WaitHealthy(context.Context, ApplyPlan) error
}

func ExecutePlan(ctx context.Context, plan ApplyPlan, lifecycle Lifecycle) error {
	if err := validateApplyPlan(plan); err != nil {
		return err
	}
	if err := lifecycle.WaitForProcess(ctx, plan.ProcessID); err != nil {
		return fmt.Errorf("wait for Igloo to stop: %w", err)
	}
	rollbacks, err := activateDirectories(plan)
	if err != nil {
		if restartErr := lifecycle.Start(ctx, plan); restartErr != nil {
			return fmt.Errorf("activate Windows update (%v) and restart existing Igloo: %w", err, restartErr)
		}
		return err
	}
	rollback := func() error { return rollbackAll(rollbacks) }
	if err := lifecycle.Start(ctx, plan); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("restart updated Igloo (%v) and restore previous files: %w", err, rollbackErr)
		}
		if restartErr := lifecycle.Start(ctx, plan); restartErr != nil {
			return fmt.Errorf("restart updated Igloo (%v) and restart rolled-back Igloo: %w", err, restartErr)
		}
		return fmt.Errorf("restart Igloo after update; previous version restored: %w", err)
	}
	if err := lifecycle.WaitHealthy(ctx, plan); err != nil {
		if stopErr := lifecycle.Stop(ctx, plan); stopErr != nil {
			return fmt.Errorf("updated Igloo failed health check (%v) and could not be stopped for rollback: %w", err, stopErr)
		}
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("updated Igloo failed health check (%v) and previous files could not be restored: %w", err, rollbackErr)
		}
		if restartErr := lifecycle.Start(ctx, plan); restartErr != nil {
			return fmt.Errorf("updated Igloo failed health check (%v) and rollback restart failed: %w", err, restartErr)
		}
		return fmt.Errorf("updated Igloo failed health check and was rolled back: %w", err)
	}
	return nil
}

func validateApplyPlan(plan ApplyPlan) error {
	root, err := filepath.Abs(plan.InstallRoot)
	if err != nil || strings.TrimSpace(plan.InstallRoot) == "" {
		return errors.New("update plan install root is invalid")
	}
	if plan.ProcessID <= 0 || (plan.AppIncoming == "" && plan.RuntimeIncoming == "") {
		return errors.New("update plan is incomplete")
	}
	for _, path := range []string{plan.StagingRoot, plan.AppIncoming, plan.RuntimeIncoming} {
		if path == "" {
			continue
		}
		if !pathWithin(root, path) {
			return fmt.Errorf("update plan path %q is outside install root", path)
		}
	}
	return nil
}

func activateDirectories(plan ApplyPlan) ([]func() error, error) {
	var rollbacks []func() error
	components := []struct {
		name     string
		incoming string
	}{
		{"app", plan.AppIncoming},
		{"runtime", plan.RuntimeIncoming},
	}
	for _, component := range components {
		if component.incoming == "" {
			continue
		}
		current := filepath.Join(plan.InstallRoot, component.name, "current")
		previous := filepath.Join(plan.InstallRoot, component.name, "previous")
		if err := os.RemoveAll(previous); err != nil {
			return nil, errors.Join(fmt.Errorf("remove previous %s version: %w", component.name, err), rollbackAll(rollbacks))
		}
		if err := os.Rename(current, previous); err != nil {
			return nil, errors.Join(fmt.Errorf("preserve current %s version: %w", component.name, err), rollbackAll(rollbacks))
		}
		if err := os.Rename(component.incoming, current); err != nil {
			restoreErr := os.Rename(previous, current)
			return nil, errors.Join(fmt.Errorf("activate new %s version: %w", component.name, err), restoreErr, rollbackAll(rollbacks))
		}
		currentCopy, previousCopy := current, previous
		rollbacks = append(rollbacks, func() error {
			failed := currentCopy + ".failed"
			_ = os.RemoveAll(failed)
			if err := os.Rename(currentCopy, failed); err != nil {
				return err
			}
			if err := os.Rename(previousCopy, currentCopy); err != nil {
				_ = os.Rename(failed, currentCopy)
				return err
			}
			_ = os.RemoveAll(failed)
			return nil
		})
	}
	return rollbacks, nil
}

func rollbackAll(rollbacks []func() error) error {
	var errs []error
	for i := len(rollbacks) - 1; i >= 0; i-- {
		if err := rollbacks[i](); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func pathWithin(root, path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, absPath)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
