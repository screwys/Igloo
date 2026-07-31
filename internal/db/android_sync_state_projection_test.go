package db

import "testing"

func TestAndroidSyncMomentsCursorStateExcludesLocalStoriesScope(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.ExecRaw(`
		INSERT INTO moments_cursors (
			scope, video_id, position_ms, sort_at_ms, updated_at_ms
		) VALUES
			('all', 'sample_all', 10, 100, 1000),
			('following', 'sample_following', 20, 200, 2000),
			('stories', 'sample_story', 30, 300, 3000)
	`); err != nil {
		t.Fatal(err)
	}

	keys, err := d.ListAndroidSyncStateKeys()
	if err != nil {
		t.Fatal(err)
	}
	gotKeys := make(map[AndroidSyncStateKey]bool, len(keys))
	for _, key := range keys {
		gotKeys[key] = true
	}
	for _, key := range []AndroidSyncStateKey{
		{OwnerKind: "moments_cursor", OwnerID: "all"},
		{OwnerKind: "moments_cursor", OwnerID: "following"},
	} {
		if !gotKeys[key] {
			t.Fatalf("state keys missing %+v: %+v", key, keys)
		}
	}
	if gotKeys[AndroidSyncStateKey{OwnerKind: "moments_cursor", OwnerID: "stories"}] {
		t.Fatalf("state keys included Android-local stories cursor: %+v", keys)
	}

	rows, err := d.ListAndroidSyncStateProjections([]AndroidSyncStateKey{
		{OwnerKind: "moments_cursor", OwnerID: "all"},
		{OwnerKind: "moments_cursor", OwnerID: "following"},
		{OwnerKind: "moments_cursor", OwnerID: "stories"},
	})
	if err != nil {
		t.Fatal(err)
	}
	gotProjections := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.OwnerKind == "moments_cursor" {
			gotProjections[row.OwnerID] = true
		}
	}
	if !gotProjections["all"] || !gotProjections["following"] {
		t.Fatalf("moments cursor projections = %+v, want all and following", gotProjections)
	}
	if gotProjections["stories"] {
		t.Fatalf("moments cursor projections included Android-local stories: %+v", gotProjections)
	}
}
