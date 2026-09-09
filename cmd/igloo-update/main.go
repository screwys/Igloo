package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/screwys/igloo/internal/windowsupdate"
)

func main() {
	planPath := flag.String("plan", "", "signed Igloo update plan prepared by the running server")
	flag.Parse()
	if *planPath == "" {
		fmt.Fprintln(os.Stderr, "igloo-update: --plan is required")
		os.Exit(2)
	}
	data, err := os.ReadFile(*planPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "igloo-update: read plan: %v\n", err)
		os.Exit(1)
	}
	var plan windowsupdate.ApplyPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintf(os.Stderr, "igloo-update: parse plan: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	logFile, err := os.OpenFile(filepath.Join(plan.InstallRoot, "updates", "update.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "igloo-update: open log: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logFile.Close() }()
	output := io.MultiWriter(logFile, os.Stderr)
	_, _ = fmt.Fprintf(output, "%s: applying Windows update\n", time.Now().Format(time.RFC3339))
	if err := windowsupdate.ExecutePlan(ctx, plan, newPlatformLifecycle()); err != nil {
		_, _ = fmt.Fprintf(output, "%s: igloo-update: %v\n", time.Now().Format(time.RFC3339), err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintf(output, "%s: Windows update completed\n", time.Now().Format(time.RFC3339))
}
