//go:build !windows

package main

func runEntrypoint() error {
	return runServer(nil, nil, false)
}
