//go:build windows

package main

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const windowsServiceName = "Igloo"

func runEntrypoint() error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect Windows service session: %w", err)
	}
	if !isService {
		return runUserServer()
	}
	return svc.Run(windowsServiceName, windowsServiceHandler{})
}

func runUserServer() error {
	name, err := windows.UTF16PtrFromString(`Local\Igloo.Server.Stop`)
	if err != nil {
		return err
	}
	event, err := windows.CreateEvent(nil, 1, 0, name)
	if event != 0 {
		defer func() { _ = windows.CloseHandle(event) }()
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create server stop event: %w", err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if result, waitErr := windows.WaitForSingleObject(event, windows.INFINITE); waitErr == nil && result == windows.WAIT_OBJECT_0 {
			close(stop)
		}
	}()
	err = runServer(stop, nil, false)
	_ = windows.SetEvent(event)
	<-done
	return err
}

type windowsServiceHandler struct{}

func (windowsServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	stop := make(chan struct{})
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runServer(stop, ready, true)
	}()

	select {
	case <-ready:
		statuses <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	case err := <-done:
		return serviceResult(err)
	}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				close(stop)
				return serviceResult(<-done)
			}
		case err := <-done:
			return serviceResult(err)
		}
	}
}

func serviceResult(err error) (bool, uint32) {
	if err == nil {
		return false, 0
	}
	if events, openErr := eventlog.Open(windowsServiceName); openErr == nil {
		_ = events.Error(1, err.Error())
		_ = events.Close()
	}
	// svc.Run reports Stopped with this code; sending Stopped earlier loses it.
	return true, 1
}
