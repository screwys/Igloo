//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "Igloo"

func runEntrypoint() error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect Windows service session: %w", err)
	}
	if !isService {
		return runServer(nil, nil, false)
	}
	return svc.Run(windowsServiceName, windowsServiceHandler{})
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
		statuses <- svc.Status{State: svc.Stopped}
		if err == nil {
			return false, 0
		}
		return false, 1
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
				if err := <-done; err != nil {
					return false, 1
				}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}
