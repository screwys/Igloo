package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestOpenMigratesAccountDetailsWithoutLosingProfile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	d, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertChannelProfile(model.ChannelProfile{ChannelID: "twitter_sample", Platform: "twitter", DisplayName: "Sample", AccountRegion: "Japan"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`DROP TRIGGER android_sync_head_channel_profiles_update;
		ALTER TABLE channel_profiles DROP COLUMN account_details_json;
		DELETE FROM schema_migrations WHERE name = '20260906_add_x_account_details'`)
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	profile, err := d.GetChannelProfile("twitter_sample")
	if err != nil || profile == nil || profile.DisplayName != "Sample" || profile.AccountRegion != "Japan" || profile.AccountDetailsJSON != "" {
		t.Fatalf("migrated profile = %+v, err=%v", profile, err)
	}
	head := requireAndroidSyncHead(t, d, "channel", profile.ChannelID)
	profile.AccountDetailsJSON = `{"source":"Web"}`
	if err := d.UpsertChannelProfile(*profile); err != nil {
		t.Fatal(err)
	}
	if next := requireAndroidSyncHead(t, d, "channel", profile.ChannelID); next.Revision <= head.Revision {
		t.Fatal("migrated account details update did not advance sync revision")
	}
}

func TestUpsertChannelProfileStoresMetadataOnly(t *testing.T) {
	d := openWritableTestDB(t)
	fetchedAt := time.UnixMilli(2000)
	profile := model.ChannelProfile{
		ChannelID:     "twitter_sample_profile",
		Platform:      "twitter",
		Handle:        "sample_profile",
		DisplayName:   "Sample Profile",
		Bio:           "sample bio",
		Followers:     42,
		AccountRegion: "Japan",
		AvatarURL:     "https://pbs.twimg.com/profile_images/1/sample.jpg",
		BannerURL:     "https://pbs.twimg.com/profile_banners/1/sample.jpg",
		FetchedAt:     &fetchedAt,
	}
	if err := d.UpsertChannelProfile(profile); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetChannelProfile(profile.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.DisplayName != profile.DisplayName || got.Bio != profile.Bio || got.Followers != 42 || got.AccountRegion != "Japan" || got.AvatarURL != "" || got.BannerURL != "" {
		t.Fatalf("profile = %+v", got)
	}
	for _, kind := range []string{"avatar", "banner"} {
		asset, err := d.GetAssetByOwnerIdentity(kind, "channel", profile.ChannelID, 0)
		if err != nil || asset != nil {
			t.Fatalf("%s asset = %+v err=%v", kind, asset, err)
		}
	}
}

func TestChannelAccountDetailsPersistAndSync(t *testing.T) {
	d := openWritableTestDB(t)
	profile := model.ChannelProfile{ChannelID: "twitter_sample", Platform: "twitter", Handle: "sample", AccountDetailsJSON: `{"source":"Japan App Store","location_accurate":false,"username_changes":0}`}
	if err := d.UpsertChannelProfile(profile); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetChannelProfile(profile.ChannelID)
	if err != nil || got == nil || got.AccountDetailsJSON != profile.AccountDetailsJSON {
		t.Fatalf("profile = %+v, err=%v", got, err)
	}
	bulk, err := d.GetTwitterChannelProfilesByHandles([]string{"sample"})
	if err != nil || bulk["sample"].AccountDetailsJSON != profile.AccountDetailsJSON {
		t.Fatalf("bulk = %+v, err=%v", bulk, err)
	}
	projections, err := d.ListAndroidSyncChannelProjections([]string{profile.ChannelID})
	if err != nil || len(projections) != 1 || projections[0].Profile.AccountDetailsJSON != profile.AccountDetailsJSON {
		t.Fatalf("sync = %+v, err=%v", projections, err)
	}
	head := requireAndroidSyncHead(t, d, "channel", profile.ChannelID)
	profile.AccountDetailsJSON = `{"source":"Web"}`
	if err := d.UpsertChannelProfile(profile); err != nil {
		t.Fatal(err)
	}
	if next := requireAndroidSyncHead(t, d, "channel", profile.ChannelID); next.Revision <= head.Revision {
		t.Fatal("account details update did not advance sync revision")
	}
}

func TestUpsertChannelProfileDoesNotMutateCanonicalAssets(t *testing.T) {
	d := openWritableTestDB(t)
	const channelID = "youtube_UC_test_profile"
	rel := filepath.Join("thumbnails", "avatars", channelID+"-ready.png")
	writeDBTestFile(t, filepath.Join(d.storage.StateRoot(), rel), []byte("ready-avatar"))
	if err := d.StoreReadyAsset(Asset{
		AssetID:   BuildAssetID("youtube", "channel", channelID, "avatar", 0),
		AssetKind: "avatar", OwnerKind: "channel", OwnerID: channelID,
		SourceURL: "https://example.test/ready-avatar.jpg", FilePath: rel,
	}, 1000); err != nil {
		t.Fatal(err)
	}
	profile := model.ChannelProfile{
		ChannelID: channelID,
		Platform:  "youtube",
		Handle:    "sample_profile",
		AvatarURL: "https://example.test/must-not-replace.jpg",
		BannerURL: "https://example.test/banner.jpg",
	}
	if err := d.UpsertChannelProfile(profile); err != nil {
		t.Fatal(err)
	}
	if banner, err := d.GetAssetByOwnerIdentity("banner", "channel", profile.ChannelID, 0); err != nil || banner != nil {
		t.Fatalf("metadata upsert created banner = %+v err=%v", banner, err)
	}
	got, err := d.GetYouTubeChannelProfileByHandle("@sample_profile")
	if err != nil || got == nil || got.BannerURL != "" || got.AvatarURL != "https://example.test/ready-avatar.jpg" {
		t.Fatalf("profile lookup = %+v err=%v", got, err)
	}
}

func TestGetTwitterChannelProfilesByHandlesReturnsVisibleIdentityOnly(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.UpsertChannelProfile(model.ChannelProfile{
		ChannelID:   "twitter_test_visible",
		Platform:    "twitter",
		Handle:      "test_visible",
		DisplayName: "Visible Name",
		AvatarURL:   "https://example.test/avatar.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	profiles, err := d.GetTwitterChannelProfilesByHandles([]string{"@TEST_VISIBLE", "@test_visible"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := profiles["test_visible"]
	if !ok || got.DisplayName != "Visible Name" || got.AvatarURL != "" {
		t.Fatalf("visible profile = %+v", got)
	}
}
