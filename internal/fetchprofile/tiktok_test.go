package fetchprofile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseTikTokAvatar(t *testing.T) {
	data, err := os.ReadFile("testdata/tiktok_avatar.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p, err := parseTikTokAvatar("user_alpha", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Platform != "tiktok" {
		t.Fatalf("platform: %q", p.Platform)
	}
	if p.ChannelID != "tiktok_user_alpha" {
		t.Fatalf("channel_id: %q", p.ChannelID)
	}
	if p.DisplayName == "" {
		t.Fatalf("display_name empty")
	}
	if p.Handle != "user_alpha" || p.DisplayName != "User Alpha" ||
		p.Bio != "Synthetic TikTok profile fixture." ||
		p.Website != "https://example.invalid/user_alpha" || !p.Verified {
		t.Fatalf("profile metadata = %+v", p)
	}
	if p.AvatarURL == "" {
		t.Fatalf("avatar_url empty")
	}
	if p.BannerURL != "" {
		t.Fatalf("tiktok has no banner, got: %q", p.BannerURL)
	}
}

func TestParseTikTokAvatarFallsBackToThumb(t *testing.T) {
	p, err := parseTikTokAvatar("user_alpha", []byte(`[[2,{"type":"avatar","uniqueId":"user_alpha","nickname":"User Alpha","avatarThumb":"https://cdn.example/avatar-thumb.jpg"}]]`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.AvatarURL != "https://cdn.example/avatar-thumb.jpg" {
		t.Fatalf("avatar_url = %q", p.AvatarURL)
	}
}

func TestParseTikTokProfilePageMapsStats(t *testing.T) {
	page := []byte(`<html><script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{"__DEFAULT_SCOPE__":{"webapp.user-detail":{"userInfo":{"user":{"uniqueId":"user_alpha","nickname":"User Alpha","signature":"Profile biography","avatarThumb":"https://cdn.example/avatar.jpg","verified":true,"bioLink":{"link":"https://example.com"}},"stats":{"followerCount":42,"followingCount":7}}}}}</script></html>`)
	p, err := parseTikTokProfilePage("user_alpha", page)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Followers != 42 || p.Following != 7 || p.Bio != "Profile biography" ||
		p.Website != "https://example.com" || !p.Verified || p.AvatarURL != "https://cdn.example/avatar.jpg" {
		t.Fatalf("profile metadata = %+v", p)
	}
}

func TestParseTikTokProfilePageMapsStringStats(t *testing.T) {
	page := []byte(`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{"__DEFAULT_SCOPE__":{"webapp.user-detail":{"userInfo":{"user":{"uniqueId":"user_alpha","nickname":"User Alpha"},"statsV2":{"followerCount":"4200","followingCount":"70"}}}}}</script>`)
	p, err := parseTikTokProfilePage("user_alpha", page)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Followers != 4200 || p.Following != 70 {
		t.Fatalf("profile stats = %+v", p)
	}
}

func TestFetchTikTokUsesFetchedProfilePageStats(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
write_pages=""
for arg in "$@"; do
  if [ "$arg" = "--write-pages" ]; then
    write_pages="yes"
  fi
done
if [ "$write_pages" != "yes" ]; then
  exit 1
fi
printf '%s' '<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{"__DEFAULT_SCOPE__":{"webapp.user-detail":{"userInfo":{"user":{"uniqueId":"user_alpha","nickname":"User Alpha","avatarThumb":"https://cdn.example/avatar.jpg"},"stats":{"followerCount":42,"followingCount":7}}}}}</script>' > profile.txt
printf '%s\n' '[[2,{"type":"avatar","uniqueId":"user_alpha","nickname":"User Alpha","avatarThumb":"https://cdn.example/avatar.jpg"}]]'
`
	path := filepath.Join(bin, "gallery-dl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write gallery-dl fixture: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p, err := FetchTikTok(context.Background(), "user_alpha")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if p.Followers != 42 || p.Following != 7 {
		t.Fatalf("profile stats = %+v", p)
	}
}

func TestParseTikTokAvatarAcceptsConcatenatedGalleryOutput(t *testing.T) {
	data, err := os.ReadFile("testdata/tiktok_avatar.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	data = append([]byte(`[[1,"https://example.test/ignored",{}]]`), data...)
	p, err := parseTikTokAvatar("user_alpha", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.ChannelID != "tiktok_user_alpha" || p.AvatarURL == "" {
		t.Fatalf("profile = %+v", p)
	}
}

func TestParseTikTokAvatarFallsBackToHandleForInvisibleNickname(t *testing.T) {
	p, err := parseTikTokAvatar("user_alpha", []byte(`[[1,{"type":"avatar","uniqueId":"user_alpha","nickname":"\uFFF4 \uFFF4"}]]`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.DisplayName != "user_alpha" {
		t.Fatalf("display_name: got %q, want handle", p.DisplayName)
	}
}

func TestParseTikTokEmpty(t *testing.T) {
	if _, err := parseTikTokAvatar("ghost", []byte("[]")); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestParseTikTokMalformed(t *testing.T) {
	if _, err := parseTikTokAvatar("ghost", []byte("not json")); err == nil {
		t.Fatalf("expected parse error")
	}
}
