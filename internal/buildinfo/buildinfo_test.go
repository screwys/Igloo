package buildinfo

import "testing"

func TestCurrentNormalizesInjectedVersion(t *testing.T) {
	oldVersion, oldRevision, oldCommit := version, bundleRevision, commit
	version, bundleRevision, commit = "v3.4.0", "19", "sample-commit"
	t.Cleanup(func() {
		version, bundleRevision, commit = oldVersion, oldRevision, oldCommit
	})

	got := Current()
	if got.Version != "3.4.0" || got.BundleRevision != "19" || got.Commit != "sample-commit" {
		t.Fatalf("Current() = %+v", got)
	}
}

func TestNormalizedDevelopmentValues(t *testing.T) {
	if got := normalized("(devel)", "dev"); got != "dev" {
		t.Fatalf("normalized development version = %q", got)
	}
}
