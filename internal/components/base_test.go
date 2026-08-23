package components

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func testStaticV(path string) string {
	return "/static/" + path + "?v=test123"
}

func newTestPageProps() PageProps {
	return PageProps{
		CSRFToken:           "test-csrf-token",
		UserRole:            "admin",
		Username:            "testuser",
		UserPlatforms:       []string{"youtube", "twitter", "tiktok"},
		PageTitle:           "Test Page",
		ActiveNav:           "videos",
		ShortcutConfig:      map[string]string{"feed.like": "l"},
		TranslateTargetLang: "en",
		TranslateSkipLangs:  "zh,ja",
		Language:            "en",
		SupportedLanguages:  []LanguageChoice{{Code: "en", Name: "English"}},
		Sidebar: model.SidebarContext{
			Groups: []model.ChannelGroup{
				{
					Title:   "Starred",
					GroupID: "starred",
					Channels: []model.Channel{
						{
							ChannelID: "youtube_test1",
							Name:      "Test Channel",
							Platform:  "youtube",
							Handle:    "test_handle",
							AvatarURL: "/api/media/avatar/youtube_test1",
							IsStarred: true,
						},
					},
				},
			},
		},
		StaticV: testStaticV,
	}
}

func renderToString(t *testing.T, c func() string) string {
	t.Helper()
	return c()
}

func renderBase(t *testing.T, p PageProps) string {
	t.Helper()
	var buf bytes.Buffer
	err := Base(p).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Base render failed: %v", err)
	}
	return buf.String()
}

func TestBaseRendersStructure(t *testing.T) {
	p := newTestPageProps()
	html := renderBase(t, p)

	checks := []struct {
		name   string
		substr string
	}{
		{"doctype", "<!doctype html>"},
		{"csrf meta", `name="csrf-token" content="test-csrf-token"`},
		{"user-role meta", `name="user-role" content="admin"`},
		{"user-platforms meta", `name="user-platforms" content="youtube,twitter,tiktok"`},
		{"translate-target meta", `name="translate-target" content="en"`},
		{"translate-skip-langs meta", `name="translate-skip-langs" content="zh,ja"`},
		{"title", "<title>Test Page</title>"},
		{"favicon", `href="/static/favicon.svg?v=test123"`},
		{"fallback theme stylesheet", `href="/static/theme.css?v=test123"`},
		{"stylesheet", `href="/static/style.css?v=test123"`},
		{"generated theme stylesheet id", `id="igloo-theme-css"`},
		{"generated theme stylesheet href", `href="/api/theme.css"`},
		{"empty-actions style", "#page-title-actions:empty"},
		{"sidebar-overlay", `id="sidebar-overlay"`},
		{"sidebar", `class="sidebar"`},
		{"sidebar id", `id="app-sidebar"`},
		{"main-content", `id="main-content"`},
		{"sidebar-toggle", `id="sidebar-toggle"`},
		{"sidebar resize handle", `id="sidebar-resize-handle"`},
		{"sidebar resize preference", `igloo.sidebar.width.v1`},
		{"floating-header", `class="floating-header"`},
		{"compact search button", `id="compact-search-btn"`},
		{"channel-settings-popover", `id="channel-settings-popover"`},
		{"add-sub-modal", `id="add-sub-modal"`},
		{"prefs-modal", `id="prefs-modal"`},
		{"import-config-modal", `id="import-config-modal"`},
		{"logs-modal", `id="logs-modal"`},
		{"confirm-modal", `id="confirm-modal"`},
		{"search-overlay", `id="search-overlay"`},
		{"modal-container", `id="modal-container"`},
		{"mini player shell", `id="mini-player-shell"`},
		{"mini player media host", `id="mini-player-media-host"`},
		{"mini player return", `id="mini-player-return"`},
		{"mini player close", `id="mini-player-close"`},
		{"mini player browse frame", `id="mini-player-browse-frame"`},
		{"i18n config", `window.IglooI18n`},
		{"shortcut config", `window._cfShortcutConfig`},
		{"web_theme.js", `js/web_theme.js?v=test123`},
		{"site_base.js", `js/site_base.js?v=test123`},
		{"video_cards.js", `js/video_cards.js?v=test123`},
		{"mini player bundle", `js/dist/mini-player.js?v=test123`},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(strings.ToLower(html), strings.ToLower(c.substr)) {
				t.Errorf("expected %q in output", c.substr)
			}
		})
	}
}

func TestBaseEmbedsSharePreferenceConfig(t *testing.T) {
	p := newTestPageProps()
	html := renderBase(t, p)

	if !strings.Contains(html, `"shareEmbedFriendlyLinks":false`) {
		t.Fatalf("base config should default shareEmbedFriendlyLinks off:\n%s", html)
	}

	p.ShareEmbedFriendlyLinks = true
	html = renderBase(t, p)
	if !strings.Contains(html, `"shareEmbedFriendlyLinks":true`) {
		t.Fatalf("base config should expose enabled shareEmbedFriendlyLinks:\n%s", html)
	}
}

func TestBaseEmbedsMiniPlayerPreferenceConfig(t *testing.T) {
	p := newTestPageProps()
	p.MiniPlayerVideosEnabled = true
	p.MiniPlayerFeedEnabled = false
	html := renderBase(t, p)

	if !strings.Contains(html, `"miniPlayerVideosEnabled":true`) {
		t.Fatalf("base config should expose the Videos mini-player preference:\n%s", html)
	}
	if !strings.Contains(html, `"miniPlayerFeedEnabled":false`) {
		t.Fatalf("base config should expose the Feed mini-player preference:\n%s", html)
	}
}

func TestPrefsBodyRendersMiniPlayerSettings(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{
		"mini_player_videos_enabled": true,
		"mini_player_feed_enabled":   false,
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, `name="mini_player_videos_enabled" value="true" checked`) {
		t.Fatalf("Videos mini-player setting should render enabled:\n%s", html)
	}
	if !strings.Contains(html, `name="mini_player_feed_enabled" value="true"`) || strings.Contains(html, `name="mini_player_feed_enabled" value="true" checked`) {
		t.Fatalf("Feed mini-player setting should render disabled:\n%s", html)
	}
}

func TestPrefsBodyRendersAppearanceThemeControls(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{
		"web_theme_id":     "catppuccin-mocha",
		"web_theme_accent": "#f38ba8",
		"web_custom_css":   ".feed-card { border-color: hotpink; }",
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	checks := []string{
		`data-prefs-tab="appearance"`,
		`name="web_theme_id"`,
		`value="catppuccin-mocha" selected`,
		`name="web_theme_accent"`,
		`type="color"`,
		`data-catppuccin-accent="mauve"`,
		`data-accent-hex="#cba6f7"`,
		`name="web_custom_css"`,
		`.feed-card { border-color: hotpink; }`,
	}
	for _, want := range checks {
		if !strings.Contains(html, want) {
			t.Fatalf("preferences body missing %q:\n%s", want, html)
		}
	}
}

func TestPrefsBodyHidesCatppuccinPillsForNonCatppuccinTheme(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{
		"web_theme_id":     "dracula",
		"web_theme_accent": "#bd93f9",
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	checks := []string{
		`value="dracula" selected`,
		`data-catppuccin-accent-row style="display:none;"`,
	}
	for _, want := range checks {
		if !strings.Contains(html, want) {
			t.Fatalf("preferences body missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `data-catppuccin-accent="mauve"`) {
		t.Fatalf("non-Catppuccin theme should not render Catppuccin accent pills:\n%s", html)
	}
}

func TestPrefsBodyRendersInstagramTaggedToggleInRightColumn(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{
		"instagram_include_tagged_default": true,
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, `name="instagram_include_tagged_default" value="true" checked`) {
		t.Fatalf("preferences body should render checked Instagram tagged toggle:\n%s", html)
	}
	translationIdx := strings.Index(html, `name="translate_auto_mode"`)
	instagramIdx := strings.Index(html, `name="instagram_fetch_delay"`)
	if translationIdx < 0 || instagramIdx < 0 {
		t.Fatalf("preferences body missing translation or Instagram controls")
	}
	if instagramIdx < translationIdx {
		t.Fatalf("Instagram section should render in the right column after translation controls")
	}
}

func TestYouTubeChannelSettingsRenderMemberOnlyOverride(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := ChannelSettingsForm(p, "youtube_sample_channel", "youtube", ChannelSettingsData{
		IncludeMemberOnly: true,
	}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `name="include_member_only" value="true" checked`) ||
		!strings.Contains(html, `Include member-only content`) {
		t.Fatalf("YouTube channel settings missing enabled member-only override:\n%s", html)
	}
}

func TestPrefsBodyGeneralTabGroupsEmbedsAndMovesBackupsLeft(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{
		"share_embed_friendly_links": true,
		"backup_enabled":             true,
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	embedsIdx := strings.Index(html, `Embeds`)
	shareIdx := strings.Index(html, `Use embed-friendly sites for sharing links`)
	if embedsIdx < 0 || shareIdx < 0 {
		t.Fatalf("preferences body should render the Embeds section with plural copy:\n%s", html)
	}
	if shareIdx < embedsIdx {
		t.Fatalf("share embed toggle should render under the Embeds header")
	}
	if strings.Contains(html, `Use embed-friendly site for sharing links`) {
		t.Fatalf("preferences body should not render the old singular embed copy")
	}

	backupIdx := strings.Index(html, `name="backup_enabled"`)
	archiveIdx := strings.Index(html, `name="archive_bookmarks"`)
	if backupIdx < 0 || archiveIdx < 0 {
		t.Fatalf("preferences body missing backup or bookmark controls")
	}
	if archiveIdx < backupIdx {
		t.Fatalf("backup controls should render in the left column before bookmark controls")
	}
}

func TestPrefsBodyRendersPersistedSidebarRouteOrder(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{
		"sidebar_route_order": "feed,discover,videos,liked,channels,bookmarks,shorts",
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `name="sidebar_route_order" value="feed,discover,videos,liked,channels,bookmarks,shorts"`) {
		t.Fatalf("preferences should preserve the configured sidebar route order:\n%s", html)
	}
}

func TestSidebarUsesConfiguredRouteOrder(t *testing.T) {
	p := newTestPageProps()
	p.UserPlatforms = []string{"youtube", "twitter"}
	p.Prefs = PrefsData{Settings: map[string]any{
		"sidebar_route_order": "feed,discover,videos,liked,channels,bookmarks,shorts",
	}}
	var buf bytes.Buffer
	if err := Sidebar(p).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	feed := strings.Index(html, `href="/feed"`)
	discover := strings.Index(html, `href="/discover"`)
	videos := strings.Index(html, `href="/videos" class="nav-item`)
	if feed < 0 || discover < 0 || videos < 0 || !(feed < discover && discover < videos) {
		t.Fatalf("sidebar routes are not rendered in the configured order:\n%s", html)
	}
}

func TestBookmarkCategoryPathsPanelUsesTallerScrollArea(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := BookmarkCategoryPathsPanel(p, nil).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, `max-height: 420px`) {
		t.Fatalf("bookmark category list should allow 1.5x more height before scrolling:\n%s", html)
	}
}

func TestBookmarkArchiveControlsAreAdminOnly(t *testing.T) {
	p := newTestPageProps()
	p.UserRole = "user"
	prefs := PrefsData{Settings: map[string]any{"archive_bookmarks": true}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Contains(html, `name="archive_bookmarks"`) || strings.Contains(html, `Save bookmarks to folder`) {
		t.Fatalf("non-admin preferences should not show bookmark archive controls:\n%s", html)
	}
	if !strings.Contains(html, `hx-get="/api/bookmark-categories"`) {
		t.Fatalf("non-admin preferences should still show category management loader:\n%s", html)
	}
}

func TestBookmarkCategoryPathsPanelHidesArchivePathsForNonAdmin(t *testing.T) {
	p := newTestPageProps()
	p.UserRole = "user"
	cats := []BookmarkCategoryDisplay{{
		ID:          1,
		Name:        "Saved",
		ArchivePath: "/archive/private",
		Slug:        "saved",
	}}
	var buf bytes.Buffer
	if err := BookmarkCategoryPathsPanel(p, cats).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Contains(html, `name="archive_path"`) || strings.Contains(html, `/archive/private`) {
		t.Fatalf("non-admin category panel leaked archive path:\n%s", html)
	}
	if !strings.Contains(html, `name="name"`) {
		t.Fatalf("non-admin category panel should keep category names editable:\n%s", html)
	}
}

func TestCookieRowsPanelRendersDisableActionWithoutRemoveAndCompactBrowserSelect(t *testing.T) {
	p := newTestPageProps()
	rows := []CookieRowData{{
		Platform:  "twitter",
		Name:      "X / Twitter",
		Exists:    true,
		Enabled:   true,
		FileCount: 2,
	}}
	var buf bytes.Buffer
	if err := CookieRowsPanel(p, rows).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	for _, want := range []string{
		`class="input cookie-browser-select"`,
		`multiple`,
		`>2 files active<`,
		`hx-post="/api/cookies/twitter/toggle"`,
		`>Disable<`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("cookie rows panel missing %q:\n%s", want, html)
		}
	}
	for _, unwanted := range []string{
		`hx-delete="/api/cookies/twitter"`,
		`>Remove<`,
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("cookie rows panel should not render %q:\n%s", unwanted, html)
		}
	}
}

func TestCookieBrowserSelectSizingOverridesGlobalInputWidth(t *testing.T) {
	css, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	text := string(css)
	globalInputIdx := strings.Index(text, "\n.input {")
	cookieSelectIdx := strings.LastIndex(text, "select.input.cookie-browser-select")
	if globalInputIdx < 0 || cookieSelectIdx < 0 {
		t.Fatalf("missing global input or cookie select sizing rule")
	}
	if cookieSelectIdx < globalInputIdx {
		t.Fatalf("cookie select sizing rule should come after the global .input width rule")
	}
	rule := text[cookieSelectIdx:]
	if end := strings.Index(rule, "}"); end >= 0 {
		rule = rule[:end]
	}
	if !strings.Contains(rule, "width: auto;") || !strings.Contains(rule, "height: 34px;") {
		t.Fatalf("cookie select sizing rule should keep dropdown button-sized, got:\n%s", rule)
	}
	themedIdx := strings.LastIndex(text, ".cookie-row-actions .themed-select {")
	if themedIdx < 0 {
		t.Fatalf("missing cookie row themed select sizing rule")
	}
	themedRule := text[themedIdx:]
	if end := strings.Index(themedRule, "}"); end >= 0 {
		themedRule = themedRule[:end]
	}
	if !strings.Contains(themedRule, "width: max-content;") {
		t.Fatalf("visible themed select should only use needed width, got:\n%s", themedRule)
	}
}

func TestLogsErrorMessagesWrapWithinTheirPanel(t *testing.T) {
	css, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	ruleStart := strings.Index(string(css), "#logs-modal .err-msg {")
	if ruleStart < 0 {
		t.Fatal("missing logs error message rule")
	}
	rule := string(css)[ruleStart:]
	if end := strings.Index(rule, "}"); end >= 0 {
		rule = rule[:end]
	}
	for _, want := range []string{"min-width: 0;", "overflow-wrap: anywhere;"} {
		if !strings.Contains(rule, want) {
			t.Errorf("logs error message rule missing %q:\n%s", want, rule)
		}
	}
}

func TestWideModalsUseTheSameDesktopScaleOnMoments(t *testing.T) {
	css, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	text := string(css)
	for _, tc := range []struct {
		selector string
		start    string
	}{
		{selector: ".modal-wide", start: ".modal-wide {"},
		{selector: ".prefs-modal-content", start: ".prefs-modal-content {\n    position: relative;"},
	} {
		start := strings.Index(text, tc.start)
		if start < 0 {
			t.Fatalf("missing %s desktop sizing rule", tc.selector)
		}
		rule := text[start:]
		if end := strings.Index(rule, "}"); end >= 0 {
			rule = rule[:end]
		}
		if !strings.Contains(rule, "--wide-modal-width: min(88vw, 1725px);") ||
			!strings.Contains(rule, "width: var(--wide-modal-width);") ||
			!strings.Contains(rule, "height: min(calc(var(--wide-modal-width) * 10 / 16), 95vh);") {
			t.Fatalf("%s should use the shared centered desktop scale, got:\n%s", tc.selector, rule)
		}
	}

	for _, selector := range []string{
		"body.shorts-open .modal-wide",
		"body.shorts-open .prefs-modal-content",
	} {
		if strings.Contains(text, selector) {
			t.Errorf("Moments should use the shared modal sizing; found %q", selector)
		}
	}
}

func TestHeaderSearchResizesConsistentlyAcrossPages(t *testing.T) {
	css, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	text := string(css)
	for _, check := range []string{
		".header-search {\n    width: clamp(84px, 8vw, 140px)",
		".header-search.is-expanded {\n    width: min(520px, 42vw)",
		"body.shorts-mode .floating-header",
		"top: max(0.5rem, env(safe-area-inset-top))",
	} {
		if !strings.Contains(text, check) {
			t.Errorf("shared responsive header search sizing missing %q", check)
		}
	}
	if strings.Contains(text, "body.shorts-mode .header-search:not(.is-expanded)") ||
		strings.Contains(text, "body.shorts-mode .header-search.is-expanded") {
		t.Fatal("Moments should not resize the shared header search to fit the story tray")
	}
}

func TestMomentsHeaderCompactsBeforeItCanOverlapTheStoryRail(t *testing.T) {
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	compactStart := strings.Index(css, "@media (max-width: 420px) {")
	if compactStart < 0 {
		t.Fatal("missing the compact Moments header boundary")
	}
	compactRules := css[compactStart:]
	for _, check := range []string{
		"body.shorts-mode .header-search",
		"body.shorts-mode .compact-header-search-btn",
		"right: calc(var(--shorts-story-tray-width) + 0.5rem)",
		"top: var(--shorts-compact-header-height)",
		"height: calc(100dvh - var(--shorts-compact-header-height))",
		"top: calc(var(--shorts-compact-header-height) + 57px)",
		"body.shorts-mode .shorts-story-tray-toggle",
	} {
		if !strings.Contains(compactRules, check) {
			t.Errorf("compact Moments header missing %q", check)
		}
	}

	siteBaseBytes, err := os.ReadFile("../../static/js/site_base.js")
	if err != nil {
		t.Fatal(err)
	}
	siteBase := string(siteBaseBytes)
	for _, check := range []string{
		"var compactSearchButton = q('#compact-search-btn')",
		"compactSearchButton.addEventListener('click'",
		"openSearchOverlay()",
	} {
		if !strings.Contains(siteBase, check) {
			t.Errorf("compact search button wiring missing %q", check)
		}
	}
}

func TestSidebarNavPlatforms(t *testing.T) {
	t.Run("all platforms", func(t *testing.T) {
		p := newTestPageProps()
		p.UserPlatforms = []string{"youtube", "twitter", "tiktok"}
		html := renderBase(t, p)

		navItems := []string{
			`href="/videos"`,
			`href="/feed"`,
			`href="/shorts"`,
			`href="/channels"`,
			`href="/bookmarks"`,
			`href="/liked"`,
		}
		for _, item := range navItems {
			if !strings.Contains(html, item) {
				t.Errorf("expected nav item %q with all platforms", item)
			}
		}
	})

	t.Run("youtube only", func(t *testing.T) {
		p := newTestPageProps()
		p.UserPlatforms = []string{"youtube"}
		html := renderBase(t, p)

		// Should have videos, channels, bookmarks
		for _, item := range []string{`href="/videos"`, `href="/channels"`, `href="/bookmarks"`} {
			if !strings.Contains(html, item) {
				t.Errorf("expected nav item %q with youtube-only", item)
			}
		}

		// Should NOT have feed, shorts, liked
		for _, item := range []string{`href="/feed"`, `href="/shorts"`, `href="/liked"`} {
			if strings.Contains(html, item) {
				t.Errorf("unexpected nav item %q with youtube-only", item)
			}
		}
	})
}

func TestSidebarChannelGroups(t *testing.T) {
	p := newTestPageProps()
	html := renderBase(t, p)

	if !strings.Contains(html, `id="group-starred"`) {
		t.Error("expected starred group")
	}
	if !strings.Contains(html, `data-channel-id="youtube_test1"`) {
		t.Error("expected channel item with youtube_test1")
	}
	if !strings.Contains(html, "Test Channel") {
		t.Error("expected channel name")
	}
	if !strings.Contains(html, "@test_handle") {
		t.Error("expected channel handle next to channel name")
	}
}

func TestSidebarGroupTitleUsesCatalog(t *testing.T) {
	p := newTestPageProps()
	p.Language = "tr"
	p.Text = map[string]string{
		"drawer_starred": "Yıldızlı",
	}
	p.Sidebar.Groups[0].Title = "Favourites"
	p.Sidebar.Groups[0].GroupID = "favourites"
	html := renderBase(t, p)

	if !strings.Contains(html, ">Yıldızlı<") {
		t.Fatalf("expected localized favourites group title:\n%s", html)
	}
	if strings.Contains(html, ">Favourites<") {
		t.Fatalf("unexpected raw favourites group title:\n%s", html)
	}
	if !strings.Contains(html, `data-i18n="drawer_starred"`) {
		t.Fatalf("expected live i18n marker for sidebar group title:\n%s", html)
	}
}

func TestSidebarPinnedVideos(t *testing.T) {
	p := newTestPageProps()
	p.Sidebar.PinnedVideos = []model.Video{
		{VideoID: "pin1", Title: "Pinned Test Video", Duration: 600, PlaybackPosition: 150},
	}
	html := renderBase(t, p)

	if !strings.Contains(html, "Pinned Videos") {
		t.Error("expected pinned section title")
	}
	if !strings.Contains(html, "/player/pin1") {
		t.Error("expected pinned video link")
	}
	if !strings.Contains(html, "Pinned Test Video") {
		t.Error("expected pinned video title")
	}
	if !strings.Contains(html, "width:25%") {
		t.Error("expected 25% progress fill for 150s of 600s")
	}
}

func TestSidebarCurrentlyWatching(t *testing.T) {
	p := newTestPageProps()
	p.Sidebar.CurrentlyWatching = []model.Video{
		{VideoID: "w1", Title: "In Progress", Duration: 1000, PlaybackPosition: 200},
	}
	html := renderBase(t, p)

	if !strings.Contains(html, "Continue Watching") {
		t.Error("expected Continue Watching section title")
	}
	if !strings.Contains(html, "width:20%") {
		t.Error("expected 20% progress fill for 200s of 1000s")
	}
}

func TestSidebarCurrentlyAvailable(t *testing.T) {
	p := newTestPageProps()
	p.Sidebar.CurrentlyAvailable = []model.Video{
		{VideoID: "a1", Title: "Temp Unpinned"},
	}
	html := renderBase(t, p)

	if !strings.Contains(html, "Currently Available") {
		t.Error("expected Currently Available section title")
	}
	if !strings.Contains(html, "Temp Unpinned") {
		t.Error("expected unpinned temp title")
	}
}

func TestPrefsModalAdminTabs(t *testing.T) {
	t.Run("admin sees users tab", func(t *testing.T) {
		// Prefs body is now HTMX lazy-loaded. Test the PrefsBody component directly.
		p := newTestPageProps()
		p.UserRole = "admin"
		prefs := PrefsData{Settings: map[string]any{"quality": "best"}}
		var buf bytes.Buffer
		if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		if !strings.Contains(html, `data-prefs-tab="users"`) {
			t.Error("expected users tab for admin")
		}
		if !strings.Contains(html, `/api/config/export`) {
			t.Error("expected export link for admin")
		}
		if !strings.Contains(html, `/api/config/export-subscriptions`) {
			t.Error("expected subscriptions export link for admin")
		}
	})

	t.Run("non-admin no users tab", func(t *testing.T) {
		p := newTestPageProps()
		p.UserRole = "user"
		prefs := PrefsData{Settings: map[string]any{"quality": "best"}}
		var buf bytes.Buffer
		if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		if strings.Contains(html, `data-prefs-tab="users"`) {
			t.Error("unexpected users tab for non-admin")
		}
		if strings.Contains(html, `/api/config/export`) {
			t.Error("unexpected export link for non-admin")
		}
	})
}

func TestPrefsPlatformSettingsTabOwnsPlatformDefaults(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{"quality": "best"}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, `data-prefs-tab="feed" type="button">Platform Settings</button>`) {
		t.Fatalf("expected settings modal feed tab to be labeled Platform Settings:\n%s", html)
	}
	if !strings.Contains(html, `data-shortcuts-sub="feed-shorts" type="button">Feed</button>`) {
		t.Fatalf("shortcuts Feed subtab should stay labeled Feed:\n%s", html)
	}
	if !strings.Contains(html, `data-sc="global.settings"`) {
		t.Fatalf("shortcuts should include the settings binding:\n%s", html)
	}

	platformPanel := strings.Index(html, `data-prefs-panel="feed"`)
	sponsorPanel := strings.Index(html, `data-prefs-panel="sponsorblock"`)
	youtubeSetting := strings.Index(html, `name="youtube_fetch_delay"`)
	if platformPanel < 0 || sponsorPanel < 0 || youtubeSetting < 0 {
		t.Fatalf("missing expected platform panel or YouTube setting:\n%s", html)
	}
	if youtubeSetting < platformPanel || youtubeSetting > sponsorPanel {
		t.Fatalf("youtube settings should render inside Platform Settings panel:\n%s", html)
	}
	for _, want := range []string{
		`name="youtube_include_member_only" value="true"`,
		`Include member-only content`,
		`name="instagram_repost_max_videos" class="input" min="1" max="300" value="15"`,
		`name="tiktok_repost_max_videos" class="input" min="1" max="300" value="15"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing default repost storage input %q:\n%s", want, html)
		}
	}
}

func TestPrefsBodyAllowsThreeSecondFetchDelay(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{"x_feed_fetch_delay": "3"}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, `id="global-setting-x-feed-fetch-delay" name="x_feed_fetch_delay" class="input" min="3"`) {
		t.Fatalf("fetch delay input should allow 3 seconds:\n%s", html)
	}
	if !strings.Contains(html, `name="x_feed_fetch_delay" class="input" min="3" max="300" value="3"`) {
		t.Fatalf("fetch delay input should preserve a 3 second value:\n%s", html)
	}
}

func TestPrefsBodyRendersXProfileHistoryLimit(t *testing.T) {
	p := newTestPageProps()
	prefs := PrefsData{Settings: map[string]any{"x_profile_history_limit": "1250"}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="global-setting-x-profile-history-limit" name="x_profile_history_limit" class="input" min="1" max="10000" value="1250"`) {
		t.Fatalf("X profile history input should preserve its value:\n%s", html)
	}
	if !strings.Contains(html, "Doesn&#39;t affect the media purge limit, except video thumbnails.") {
		t.Fatalf("X profile history input should explain media retention:\n%s", html)
	}
}

func TestServerDashboardKeepsRawServerLogOutsideLivePoll(t *testing.T) {
	p := newTestPageProps()
	data := ServerDashboardData{
		UptimeText:     "1m",
		UptimeStarted:  "2026-05-02 01:00:00",
		MemoryHistory:  []float64{1, 2, 3},
		ChannelsByPlat: map[string]int{},
		Activity: []ServerActivityEntry{{
			Time:    "01:00:00",
			Status:  "info",
			Source:  "worker",
			Message: "activity stays concise",
		}},
	}
	var buf bytes.Buffer
	if err := ServerDashboardLive(p, data).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	liveHTML := buf.String()
	if !strings.Contains(liveHTML, `activity stays concise`) {
		t.Fatalf("activity log should keep rendering existing activity entries:\n%s", liveHTML)
	}
	if strings.Contains(liveHTML, `sv-raw-log-section`) {
		t.Fatalf("live poll must not contain the raw server log:\n%s", liveHTML)
	}
	if !strings.Contains(liveHTML, `id="sv-workers-content" hx-swap-oob="innerHTML"`) {
		t.Fatalf("live poll should update workers out of band:\n%s", liveHTML)
	}

	buf.Reset()
	if err := logsServerPanel(p).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	shellHTML := buf.String()
	rawIdx := strings.Index(shellHTML, `id="sv-raw-log-section"`)
	if rawIdx < 0 {
		t.Fatalf("server shell should include a stable raw log section:\n%s", shellHTML)
	}
	rawHTML := shellHTML[rawIdx:]
	if !strings.Contains(rawHTML, `hx-get="/api/logs/server/read?type=server&lines=80&filter_noise=1&fmt=html"`) || !strings.Contains(rawHTML, `hx-trigger="logs-server-raw-load"`) {
		t.Fatalf("raw server log should load only from its own one-shot trigger:\n%s", rawHTML)
	}
	if strings.Contains(rawHTML, `logs-poll`) {
		t.Fatalf("raw server log must not be attached to the live polling trigger:\n%s", rawHTML)
	}
}

func TestServerDashboardLiveUsesReadableWorkerSummaryAndExpandableDetail(t *testing.T) {
	p := newTestPageProps()
	data := ServerDashboardData{Processes: []ServerProcessCard{{
		Name:    "feed_scoring",
		Status:  "running",
		Summary: "Scored 21 · refilled 500 · 2000 candidates · 3.2s",
		Detail:  "scored=21 refill=500 candidates=2000 replies=4/5 snap=1836/2.999s query=575ms build=4ms write=120ms total=3.2s top=[long diagnostic payload]",
	}}}
	var buf bytes.Buffer
	if err := ServerDashboardLive(p, data).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"Feed ranking", "Scored 21", "<details", "top=[long diagnostic payload]", `title="feed_scoring"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("live dashboard missing %q:\n%s", want, html)
		}
	}
}

func TestPrefsUILanguagePreviewAndSaveDoesNotReloadPage(t *testing.T) {
	p := newTestPageProps()
	p.SupportedLanguages = []LanguageChoice{
		{Code: "auto", Name: "Automatic"},
		{Code: "en", Name: "English"},
		{Code: "tr", Name: "Turkish"},
	}
	prefs := PrefsData{Settings: map[string]any{
		"quality":                "best",
		"ui_language":            "tr",
		"_persisted_ui_language": "en",
	}}
	var buf bytes.Buffer
	if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Contains(html, "window.location.reload") {
		t.Fatalf("language preferences should not force a page reload:\n%s", html)
	}
	if !strings.Contains(html, "handlePrefsAfterRequest.call(this,event") {
		t.Fatalf("save status handler should route through the shared preferences handler:\n%s", html)
	}
	if !strings.Contains(html, "previewLanguage(this.value)") {
		t.Fatalf("language select should preview the selected catalog before save:\n%s", html)
	}
	if strings.Contains(html, "/api/settings/form?lang=") {
		t.Fatalf("language preview should not reload the preferences form:\n%s", html)
	}
	for _, want := range []string{
		`data-i18n-scope="prefs"`,
		`id="prefs-unsaved-reminder"`,
		`data-i18n="status_save_preferences_to_apply"`,
		`data-i18n="action_save_preferences"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("preferences form missing %q:\n%s", want, html)
		}
	}
}

func TestPrefsFeedTranslateLookaheadVisibility(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		wantHidden bool
	}{
		{name: "lazy shows lookahead", mode: "lazy", wantHidden: false},
		{name: "manual hides lookahead", mode: "off", wantHidden: true},
		{name: "background hides lookahead", mode: "background", wantHidden: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPageProps()
			prefs := PrefsData{Settings: map[string]any{
				"quality":             "best",
				"translate_auto_mode": tc.mode,
			}}
			var buf bytes.Buffer
			if err := PrefsBody(p, prefs).Render(context.Background(), &buf); err != nil {
				t.Fatal(err)
			}
			html := buf.String()
			hiddenMarker := `id="translate-lookahead-config" style="display:none;"`
			if tc.wantHidden && !strings.Contains(html, hiddenMarker) {
				t.Fatalf("expected lookahead row to be hidden for mode %q:\n%s", tc.mode, html)
			}
			if !tc.wantHidden && strings.Contains(html, hiddenMarker) {
				t.Fatalf("expected lookahead row to be visible for mode %q:\n%s", tc.mode, html)
			}
		})
	}
}

func TestHeaderElements(t *testing.T) {
	p := newTestPageProps()
	html := renderBase(t, p)

	checks := []string{
		`id="global-search-input"`,
		`id="global-search-clear"`,
		`id="search-dropdown"`,
		`id="stop-play-container"`,
		`id="prefs-btn"`,
	}
	for _, c := range checks {
		if !strings.Contains(html, c) {
			t.Errorf("expected header element %q", c)
		}
	}
	if !strings.Contains(html, `id="stop-play-btn"`) {
		t.Error("expected the download toggle to be present in the initial page")
	}
	if strings.Contains(html, `hx-get="/api/stop-play-btn"`) {
		t.Error("expected the initial page to render the download toggle without a follow-up request")
	}
}

func TestLogsModalTabs(t *testing.T) {
	p := newTestPageProps()
	html := renderBase(t, p)

	tabs := []string{
		`data-logs-tab="server"`,
		`data-logs-tab="android"`,
		`data-logs-tab="downloads"`,
		`data-logs-tab="twitter"`,
		`data-logs-panel="server"`,
		`data-logs-panel="android"`,
		`data-logs-panel="downloads"`,
		`data-logs-panel="twitter"`,
	}
	for _, tab := range tabs {
		if !strings.Contains(html, tab) {
			t.Errorf("expected logs modal element %q", tab)
		}
	}
}

func TestTranslateTargetDefault(t *testing.T) {
	p := newTestPageProps()
	p.TranslateTargetLang = ""
	html := renderBase(t, p)

	if !strings.Contains(html, `name="translate-target" content="en"`) {
		t.Error("expected default translate-target of 'en'")
	}
}
