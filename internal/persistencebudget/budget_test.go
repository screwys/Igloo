package persistencebudget

import "testing"

func TestEvaluateWarnsForUnclassifiedTables(t *testing.T) {
	warnings := Evaluate([]LifecycleGroup{{
		Lifecycle: "unclassified",
		Tables:    1,
		Rows:      3,
		Bytes:     4096,
	}})
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want one unclassified warning", warnings)
	}
	if warnings[0].Lifecycle != "unclassified" || warnings[0].Code != "unclassified_tables" {
		t.Fatalf("warning = %+v, want unclassified_tables", warnings[0])
	}
}

func TestEvaluateWarnsForOversizedDerivedCache(t *testing.T) {
	warnings := Evaluate([]LifecycleGroup{
		{Lifecycle: "archive", Tables: 2, Rows: 100, Bytes: 64 << 20},
		{Lifecycle: "maintained_state", Tables: 1, Rows: 100, Bytes: 64 << 20},
		{Lifecycle: "derived_cache", Tables: 2, Rows: 1_000_000, Bytes: 2 << 30},
	})
	found := false
	for _, warning := range warnings {
		if warning.Lifecycle == "derived_cache" && warning.Code == "derived_cache_size" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want derived_cache_size", warnings)
	}
}

func TestEvaluateDoesNotWarnForSmallBoundedGroups(t *testing.T) {
	warnings := Evaluate([]LifecycleGroup{
		{Lifecycle: "archive", Tables: 2, Rows: 100, Bytes: 128 << 20},
		{Lifecycle: "maintained_state", Tables: 1, Rows: 100, Bytes: 64 << 20},
		{Lifecycle: "derived_cache", Tables: 2, Rows: 1_000, Bytes: 64 << 20},
		{Lifecycle: "queue", Tables: 2, Rows: 100, Bytes: 1 << 20},
		{Lifecycle: "diagnostic", Tables: 2, Rows: 100, Bytes: 1 << 20},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}
}
