package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
	if err := windowsupdate.ExecutePlan(ctx, plan, newPlatformLifecycle()); err != nil {
		fmt.Fprintf(os.Stderr, "igloo-update: %v\n", err)
		os.Exit(1)
	}
}
