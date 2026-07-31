package components

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestShortsPageRendersFullSkeletonListWithoutPaging(t *testing.T) {
	p := newTestPageProps()
	p.ActiveNav = "shorts"
	p.ESBundle = "js/dist/shorts.js"
	videos := []model.Video{
		{VideoID: "short_001", ChannelID: "tiktok_sample", Title: "Short 001", Platform: "tiktok"},
		{VideoID: "short_002", Title: "Short 002", Platform: "tiktok"},
		{VideoID: "short_003", Title: "Short 003", Platform: "tiktok"},
	}
	pager := model.Pager{Page: 1, PerPage: 10000, Total: 3}

	var buf bytes.Buffer
	if err := ShortsPage(p, videos, nil, false, pager, "", "following", 2).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Contains(html, `close-shorts-btn`) {
		t.Fatal("Moments should use the mode-aware Grid control instead of a second close button")
	}

	if strings.Contains(html, `class="js-infinite-scroll"`) || strings.Contains(html, `data-next-url`) {
		t.Fatal("shorts page should not use scroll pagination")
	}
	if !strings.Contains(html, `data-hydrate-batch-size="2"`) {
		t.Fatalf("missing card window size: %s", html)
	}
	if !strings.Contains(html, `data-video-id="short_001"`) || !strings.Contains(html, `data-video-title="Short 001"`) {
		t.Fatal("initial hydrated card missing")
	}
	for i, id := range []string{"short_002", "short_003"} {
		if !strings.Contains(html, `data-video-id="`+id+`"`) ||
			!strings.Contains(html, `data-shorts-card-skeleton="1"`) ||
			!strings.Contains(html, fmt.Sprintf(`data-card-index="%d"`, i+1)) {
			t.Fatalf("missing skeleton card for %s", id)
		}
	}
}

func TestShortsPageRendersStoriesTabTrigger(t *testing.T) {
	p := newTestPageProps()
	p.ActiveNav = "shorts"
	p.ESBundle = "js/dist/shorts.js"
	pager := model.Pager{Page: 1, PerPage: 10000, Total: 0}

	var buf bytes.Buffer
	if err := ShortsPage(p, nil, nil, true, pager, "", "all", 2).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()

	if !strings.Contains(html, `href="/shorts?tab=stories"`) {
		t.Fatalf("stories tab trigger missing: %s", html)
	}
	if strings.Contains(html, `shorts-tab-dot`) {
		t.Fatalf("stories tab should not render the dot indicator: %s", html)
	}
}

func TestShortsPlayerHeaderRendersStoriesTabTrigger(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)

	if !strings.Contains(src, `href="/shorts?tab=stories"`) {
		t.Fatal("shorts player header should include the Stories tab trigger for the story tray")
	}
	if !strings.Contains(src, "(_state && _state.storyMode) ? 'stories'") {
		t.Fatal("shorts player header should select the Stories tab while story playback is active")
	}
}

func TestShortsPlayerLongPressUsesMomentMutationOwners(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	for _, check := range []string{
		"function bindMomentLongPress(entry)",
		"if (!wrapper || !entry.data) return",
		"/api/mutations/channel_setting",
		"field: 'include_reposts'",
		"/api/mutations/mute",
		"/api/mutations/follow",
		"function openMomentActions(entry)",
		"function momentAccountHandleLabel(channelID, rawHandle)",
		"momentAccountHandleLabel(reposterID, data.repostHandle)",
		"momentAccountHandleLabel(authorID)",
		"action_visit_profile_of_account",
		"if (!data.channelFollowed && authorID)",
		"wrapper.appendChild(overlay)",
		"wrapper.classList.add('moment-actions-open')",
		"if (event.target !== overlay) return\n    event.preventDefault()\n    event.stopPropagation()\n    closeMomentActions()",
		"event.target.closest('.moment-actions-overlay')) return",
		"shareShort(data)",
		"else if (data.channelFollowed && authorID)",
		"advanceMomentsAfterAction(entry)",
		"function finishMomentUnfollow(entry, channelId, label, message)",
		"finishMomentUnfollow(entry, reposterID, reposterLabel)",
		"finishMomentUnfollow(entry, channelId, label, payload && payload.message)",
		"followShortAuthor(entryObj, followBtn)",
		"function followShortAuthor(entry, btn)",
	} {
		if !strings.Contains(src, check) {
			t.Errorf("Moment long-press action wiring missing %q", check)
		}
	}
	if strings.Contains(src, "window.location.reload") {
		t.Fatal("Moment actions should advance without reloading the page")
	}

	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	overlayBody := cssRuleBody(t, css, ".moment-actions-overlay")
	for _, check := range []string{"position: absolute", "inset: 0"} {
		if !strings.Contains(overlayBody, check) {
			t.Errorf("Moment actions should stay centered inside the media frame; missing %q in %s", check, overlayBody)
		}
	}
	chromeSelector := ".shorts-video-wrapper.moment-actions-open > :not(.native-short-video):not(.shorts-video-poster-frame):not(.slideshow-container):not(.slideshow-audio):not(.moment-actions-overlay)"
	if chromeBody := cssRuleBody(t, css, chromeSelector); !strings.Contains(chromeBody, "visibility: hidden") {
		t.Errorf("Moment actions should hide the player chrome: %s", chromeBody)
	}

	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	for _, check := range []string{
		"function advanceAfterMomentAction(entry)",
		"tabGridCache.delete(requestTab)",
		"loadTabSnapshot(requestTab)",
		"fetchShortsCursorFromServer(requestTab)",
		"var targetID = String(cursor && cursor.video_id || '').trim()",
		"if (nextIndex >= 0 && targetID === entryID) nextIndex += 1",
		"if (nextIndex < 0) {",
		"showGrid()",
		"openOverlayAtIndex(nextIndex, true, { persistCursor: false })",
	} {
		if !strings.Contains(indexSrc, check) {
			t.Errorf("Moment action advance wiring missing %q", check)
		}
	}
	if strings.Contains(indexSrc, "nextIndex = Math.min(Math.max(0, activeIndex)") {
		t.Fatal("Moment actions must not fall back to an earlier item")
	}
	if strings.Contains(indexSrc, "var followingIDs = state.cards.slice") {
		t.Fatal("Moment actions must not carry pre-mutation DOM order across a server reorder")
	}
}

func TestShortsStoryTrayOpensByDefaultForNormalMoments(t *testing.T) {
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	overlayBytes, err := os.ReadFile("../../static/js/src/shorts/overlay.js")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	overlaySrc := string(overlayBytes)

	for _, check := range []string{
		"function openDefaultStoryTray()",
		"if (currentTab === 'stories' || state.storyMode || !state.overlayOpen) return",
		"afterOverlayOpen: openDefaultStoryTray",
	} {
		if !strings.Contains(indexSrc, check) {
			t.Errorf("default story tray wiring missing %q", check)
		}
	}
	if !strings.Contains(overlaySrc, "typeof _fns.afterOverlayOpen === 'function'") {
		t.Fatal("overlay should call the post-open hook after Moments opens")
	}
}

func TestShortsStoryGridButtonLabelsStoryPlaybackAsMoments(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	checks := []string{
		"function storyGridButtonLabel()",
		"state.storyMode ? t('nav_moments', 'Moments') : t('action_grid', 'Grid')",
		"function updateStoryGridButton()",
		"function activateStoryGridButton()",
		"exitStoryMode({ restore: true })",
		"updateStoryGridButton()",
	}
	for _, check := range checks {
		if !strings.Contains(src, check) {
			t.Errorf("shorts story grid button wiring missing %q", check)
		}
	}
}

func TestShortsStoryTabOpensFirstTrayStory(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	checks := []string{
		"function openFirstStoryFromTray()",
		"function firstStoryTrayRow()",
		"return Promise.resolve(openStoryRow(row, { continueAcrossAccounts: true }))",
		"if (state.overlayOpen && tab === 'stories')",
		"openFirstStoryFromTray()",
	}
	for _, check := range checks {
		if !strings.Contains(src, check) {
			t.Errorf("story tab first-story wiring missing %q", check)
		}
	}
}

func TestShortsStoryModePreviousTabExitsWithoutSwitchingTabs(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	checks := []string{
		"if (state.storyMode)",
		"if (tab === currentTab)",
		"exitStoryMode({ restore: true })",
	}
	for _, check := range checks {
		if !strings.Contains(src, check) {
			t.Errorf("story mode tab return wiring missing %q", check)
		}
	}
}

func TestShortsStoryAvatarOpenDoesNotReuseSidebarQueue(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	checks := []string{
		"storyContinueAcrossAccounts: false",
		"continueAcrossAccounts: true",
		"continueAcrossAccounts: false",
		"if (!state.storyContinueAcrossAccounts) return false",
		"state.storyContinueAcrossAccounts = !!opts.continueAcrossAccounts",
		"state.storyContinueAcrossAccounts = false",
	}
	for _, check := range checks {
		if !strings.Contains(src, check) {
			t.Errorf("shorts story queue ownership missing %q", check)
		}
	}
}

func TestShortsStoryModeAutoAdvancesWithoutChangingMomentAutoplay(t *testing.T) {
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	playbackBytes, err := os.ReadFile("../../static/js/src/shorts/playback.js")
	if err != nil {
		t.Fatal(err)
	}
	overlayBytes, err := os.ReadFile("../../static/js/src/shorts/overlay.js")
	if err != nil {
		t.Fatal(err)
	}
	itemsBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	playbackSrc := string(playbackBytes)
	overlaySrc := string(overlayBytes)
	itemsSrc := string(itemsBytes)

	if !strings.Contains(indexSrc, "state.autoPlayNext = false") {
		t.Fatal("story mode should preserve the normal moments autoplay setting while it is open")
	}
	for _, check := range []string{
		"return !!(_state && (_state.storyMode || _state.autoPlayNext))",
		"if (autoAdvanceEnabled()) _goNext()",
	} {
		if !strings.Contains(playbackSrc, check) {
			t.Errorf("story playback auto-advance missing %q", check)
		}
	}
	for _, check := range []string{
		"function autoAdvanceEnabled()",
		"return !!(_state && (_state.storyMode || _state.autoPlayNext))",
		"slideshowAudio.loop = !autoAdvanceEnabled()",
		"video.loop = !autoAdvanceEnabled()",
		"if (autoAdvanceEnabled()) _fns.goNext()",
	} {
		if !strings.Contains(itemsSrc, check) {
			t.Errorf("story media auto-advance wiring missing %q", check)
		}
	}
	for _, check := range []string{
		"var autoAdvance = _state.storyMode || _state.autoPlayNext",
		"refs.autoplayBtn.classList.toggle('active', autoAdvance && isCurrent)",
		"autoAdvance ? t('state_on', 'ON') : t('state_off', 'OFF')",
	} {
		if !strings.Contains(overlaySrc, check) {
			t.Errorf("story autoplay display state missing %q", check)
		}
	}
	for _, bad := range []string{
		"if (_state.storyMode && !audio) return",
		"slideshowAudio.loop = !_state.autoPlayNext",
		"video.loop = !_state.autoPlayNext",
		"if (_state.autoPlayNext) _fns.goNext()",
	} {
		if strings.Contains(playbackSrc, bad) || strings.Contains(itemsSrc, bad) {
			t.Fatalf("story media should not loop through normal autoplay-only wiring: found %q", bad)
		}
	}
}

func TestShortsStoryNextExitsWhenQueueIsExhausted(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	start := strings.Index(src, "function goStoryNextManual()")
	if start < 0 {
		t.Fatal("goStoryNextManual missing")
	}
	end := strings.Index(src[start:], "function goStoryPrevManual()")
	if end < 0 {
		t.Fatal("goStoryPrevManual should follow goStoryNextManual")
	}
	fn := src[start : start+end]
	for _, check := range []string{
		"if (state.currentIndex < state.cards.length - 1)",
		"scrollToIndex(state.currentIndex + 1, 'instant')",
		"if (openNextQueuedStory()) return",
		"showGrid()",
	} {
		if !strings.Contains(fn, check) {
			t.Errorf("story next exhaustion behavior missing %q", check)
		}
	}
}

func TestShortsStoryClicksNavigateWithoutVisualArrowButtons(t *testing.T) {
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	itemsBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	itemsSrc := string(itemsBytes)
	css := string(cssBytes)

	for _, check := range []string{
		"function navigateStoryFromClick(entry, event)",
		"if (!_state || !_state.storyMode || !_fns) return false",
		"if (navigateStoryFromClick(entryObj, e)) return",
		"if (typeof _fns.goStoryPrev === 'function') _fns.goStoryPrev()",
		"if (typeof _fns.goStoryNext === 'function')",
	} {
		if !strings.Contains(itemsSrc, check) {
			t.Errorf("story click navigation missing %q", check)
		}
	}
	for _, check := range []string{
		"goStoryNext: goStoryNextManual",
		"goStoryPrev: goStoryPrevManual",
		"if (state.storyMode && event.key === 'ArrowRight')",
		"if (state.storyMode && event.key === 'ArrowLeft')",
	} {
		if !strings.Contains(indexSrc, check) {
			t.Errorf("story navigation wiring missing %q", check)
		}
	}
	if !strings.Contains(itemsSrc, "toggleShortPlayback(entryObj)") {
		t.Fatal("normal moments should keep click-to-toggle playback")
	}
	for _, forbidden := range []string{
		"shorts-story-arrow",
		"data-story-action",
		"onStoryPlayerControlClick",
	} {
		if strings.Contains(indexSrc, forbidden) || strings.Contains(itemsSrc, forbidden) || strings.Contains(css, forbidden) {
			t.Errorf("story visual arrow control should be removed; found %q", forbidden)
		}
	}
}

func TestShortsArrowKeysPreferSlideshowSlidesBeforeStoryNavigation(t *testing.T) {
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	playbackBytes, err := os.ReadFile("../../static/js/src/shorts/playback.js")
	if err != nil {
		t.Fatal(err)
	}
	itemsBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	playbackSrc := string(playbackBytes)
	itemsSrc := string(itemsBytes)

	if !strings.Contains(playbackSrc, "export function stepSlideshow(entry, delta)") {
		t.Fatal("shorts playback should expose a shared manual slideshow step helper")
	}
	for _, check := range []string{
		"if (!slideshow || slideshow.count <= 1) return false",
		"if (next < 0 || next >= slideshow.count) return false",
		"setSlideshowIndex(entry, next)",
		"return true",
	} {
		if !strings.Contains(playbackSrc, check) {
			t.Errorf("manual slideshow step helper missing %q", check)
		}
	}
	if !strings.Contains(itemsSrc, "stepSlideshow({ refs: { slideshow: slideshow } }, -1)") ||
		!strings.Contains(itemsSrc, "stepSlideshow({ refs: { slideshow: slideshow } }, 1)") {
		t.Fatal("slide buttons should use the same manual slideshow step helper as keyboard navigation")
	}
	slideStart := strings.Index(indexSrc, "if (event.key === 'ArrowLeft' || event.key === 'ArrowRight')")
	storyStart := strings.Index(indexSrc, "if (state.storyMode && event.key === 'ArrowRight')")
	if slideStart < 0 {
		t.Fatal("shorts keyboard handler should intercept left/right arrows for slideshow slides")
	}
	if storyStart < 0 {
		t.Fatal("story right-arrow navigation missing")
	}
	if slideStart > storyStart {
		t.Fatal("slideshow arrow handling must run before story navigation")
	}
	slideBlockEnd := strings.Index(indexSrc[slideStart:], "if (state.storyMode && event.key === 'ArrowRight')")
	if slideBlockEnd < 0 {
		t.Fatal("story right-arrow navigation should follow slideshow arrow handling")
	}
	slideBlock := indexSrc[slideStart : slideStart+slideBlockEnd]
	for _, check := range []string{
		"var slideDelta = event.key === 'ArrowRight' ? 1 : -1",
		"if (entry && stepSlideshow(entry, slideDelta))",
		"event.preventDefault()",
		"return",
	} {
		if !strings.Contains(slideBlock, check) {
			t.Errorf("shorts keyboard slideshow handling missing %q", check)
		}
	}
}

func TestShortsVerticalMomentsUseControlledDeckLayout(t *testing.T) {
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	overlayBytes, err := os.ReadFile("../../static/js/src/shorts/overlay.js")
	if err != nil {
		t.Fatal(err)
	}
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	overlaySrc := string(overlayBytes)
	indexSrc := string(indexBytes)
	layoutBody := cssRuleBody(t, css, ".shorts-layout")
	containerBody := cssRuleBody(t, css, ".shorts-container")
	itemBody := cssRuleBody(t, css, ".shorts-item")
	storyContainerBody := cssRuleBody(t, css, ".shorts-layout.shorts-story-mode .shorts-container")
	storyItemBody := cssRuleBody(t, css, ".shorts-layout.shorts-story-mode .shorts-item")
	if !strings.Contains(layoutBody, "left: var(--sidebar-width)") {
		t.Errorf("Moments should center inside the space after the sidebar: %s", layoutBody)
	}

	for _, check := range []string{
		"overflow: hidden",
		"touch-action: none",
		"overscroll-behavior-y: contain",
	} {
		if !strings.Contains(containerBody, check) {
			t.Errorf(".shorts-container controlled deck layout missing %q in %s", check, containerBody)
		}
	}
	for _, check := range []string{
		"position: absolute",
		"inset: 0",
		"will-change: transform",
	} {
		if !strings.Contains(itemBody, check) {
			t.Errorf(".shorts-item controlled deck panel missing %q in %s", check, itemBody)
		}
	}
	for _, check := range []string{
		"overflow-x: auto",
		"scroll-snap-type: x mandatory",
		"touch-action: pan-x",
	} {
		if !strings.Contains(storyContainerBody, check) {
			t.Errorf("story mode should retain horizontal native scrolling; missing %q in %s", check, storyContainerBody)
		}
	}
	for _, check := range []string{
		"position: relative",
		"scroll-snap-align: start",
		"scroll-snap-stop: always",
	} {
		if !strings.Contains(storyItemBody, check) {
			t.Errorf("story mode item should retain snap behavior; missing %q in %s", check, storyItemBody)
		}
	}
	for _, bad := range []string{
		"_dom.shortsContainer.style.overflowY = 'auto'",
		"_dom.shortsContainer.style.scrollSnapType = 'y mandatory'",
		"entry.el.scrollIntoView({ block: 'start'",
	} {
		if strings.Contains(overlaySrc, bad) {
			t.Errorf("vertical Moments should not use native scroll-snap path; found %q", bad)
		}
	}
	for _, check := range []string{
		"function startDeckTransition(index)",
		"function completeDeckTransition()",
		"recordShortsDebugEvent(target, 'deck:transition-start'",
		"_dom.shortsContainer.style.scrollSnapType = 'none'",
		"_dom.shortsContainer.style.touchAction = 'none'",
	} {
		if !strings.Contains(overlaySrc, check) {
			t.Errorf("controlled deck wiring missing %q", check)
		}
	}
	for _, check := range []string{
		"wheelLockTimer",
		"function keepWheelLocked()",
		"layout.addEventListener('touchmove', onTouchMove, { passive: false })",
	} {
		if !strings.Contains(indexSrc, check) {
			t.Errorf("desktop/touch deck input guard missing %q", check)
		}
	}
}

func TestShortsStoryTrayKeepsNativeWheelScrolling(t *testing.T) {
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	wheelStart := strings.Index(indexSrc, "function onWheel(event)")
	wheelEnd := strings.Index(indexSrc[wheelStart:], "function onTouchStart(event)")
	if wheelStart < 0 || wheelEnd < 0 {
		t.Fatal("missing Moments wheel handler")
	}
	wheelHandler := indexSrc[wheelStart : wheelStart+wheelEnd]
	trayGuard := strings.Index(wheelHandler, "event.target.closest('.shorts-story-tray')")
	preventDefault := strings.Index(wheelHandler, "event.preventDefault()")
	if trayGuard < 0 || preventDefault < 0 || trayGuard > preventDefault {
		t.Fatalf("story tray wheel events should bypass Moments navigation before preventDefault:\n%s", wheelHandler)
	}
	trayBody := cssRuleBody(t, string(cssBytes), ".shorts-story-tray-body")
	if !strings.Contains(trayBody, "overflow-y: auto") {
		t.Fatalf("story tray body should remain a native vertical scroller: %s", trayBody)
	}
}

func TestShortsStoryTrayUsesRemainingWidthBeforeCompactingItsContents(t *testing.T) {
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	compactStart := strings.Index(css, "@container story-tray (max-width: 320px) {")
	if compactStart < 0 {
		t.Fatal("missing story tray compact layout boundary")
	}
	compactEnd := strings.Index(css[compactStart:], "/* Lightweight placeholder poster")
	if compactEnd < 0 {
		t.Fatal("missing end of story tray compact layout")
	}
	compactRules := css[compactStart : compactStart+compactEnd]
	for _, check := range []string{
		".shorts-story-tray-body .story-channel-meta",
		"grid-template-columns: 48px minmax(0, 1fr)",
	} {
		if !strings.Contains(compactRules, check) {
			t.Errorf("story tray compact layout missing %q", check)
		}
	}
	for _, check := range []string{
		"--shorts-story-tray-width: clamp(76px, calc(100vw - var(--sidebar-width) - 750px - 2rem), 390px)",
		"width: min(var(--shorts-story-tray-width), 92vw)",
		"body.shorts-open:has(.shorts-story-tray.open) .shorts-layout",
		"right: var(--shorts-story-tray-width)",
		"container-name: story-tray",
		"shorts-story-tray-resize-handle",
		"top: calc(max(0.5rem, env(safe-area-inset-top)) + 58px)",
		"flex: 0 0 58px",
		"min-height: 58px",
		".shorts-story-tray-header h2 {\n    display: flex;\n    align-items: center;\n    height: 42px",
	} {
		if !strings.Contains(css, check) {
			t.Errorf("fluid story tray layout missing %q", check)
		}
	}

	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	indexSrc := string(indexBytes)
	for _, check := range []string{
		"igloo.story-tray.width.v1",
		"function setStoryTrayWidth(width, persist)",
		"setPointerCapture(event.pointerId)",
		"window.innerWidth - event.clientX",
		"function updateStoryTrayTitleCollision()",
		"function observeStoryTrayHeader(tray)",
		"state.storyTrayHeaderObserver.observe(tray)",
		"state.storyTrayHeaderObserver.observe(floatingHeader)",
		"titleRight + clearance >= floatingHeaderRect.left",
	} {
		if !strings.Contains(indexSrc, check) {
			t.Errorf("resizable story tray missing %q", check)
		}
	}
	if strings.Contains(compactRules, ".shorts-story-grid-btn {") ||
		strings.Contains(compactRules, ".shorts-story-grid-btn span") {
		t.Fatal("Grid should keep its full size outside the resizable story tray")
	}
	if strings.Contains(css, "story-tray-compact") || strings.Contains(indexSrc, "story-tray-compact") {
		t.Fatal("resizing the story tray should not hide global header controls")
	}
	collisionRule := cssRuleBody(t, css, ".shorts-story-tray.story-title-collides .shorts-story-tray-header h2")
	for _, check := range []string{"position: absolute", "width: 1px", "clip-path: inset(50%)"} {
		if !strings.Contains(collisionRule, check) {
			t.Errorf("colliding Stories title should hide without moving its header; missing %q in %s", check, collisionRule)
		}
	}
	if strings.Contains(compactRules, ".shorts-story-tray-header h2") {
		t.Fatal("Stories title visibility should follow measured header collision, not a fixed tray width")
	}
}

func TestShortsMediaEdgesDoNotExposeWrapperBackgroundDuringScrollSnap(t *testing.T) {
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	wrapperBody := cssRuleBody(t, css, ".shorts-video-wrapper")
	videoBody := cssRuleBody(t, css, ".shorts-video-wrapper video")
	nativeVideoBody := cssRuleBody(t, css, ".native-short-video")
	slideImageBody := cssRuleBody(t, css, ".slide-image")

	for _, check := range []string{
		"overflow: hidden",
		"border-radius: 0",
		"background: #000",
	} {
		if !strings.Contains(wrapperBody, check) {
			t.Errorf(".shorts-video-wrapper should avoid visible clipped edges; missing %q in %s", check, wrapperBody)
		}
	}
	for _, check := range []string{
		"display: block",
		"width: 100%",
		"height: 100%",
	} {
		if !strings.Contains(videoBody, check) {
			t.Errorf(".shorts-video-wrapper video should fill without inline baseline gaps; missing %q in %s", check, videoBody)
		}
		if !strings.Contains(nativeVideoBody, check) {
			t.Errorf(".native-short-video should fill without inline baseline gaps; missing %q in %s", check, nativeVideoBody)
		}
		if !strings.Contains(slideImageBody, check) {
			t.Errorf(".slide-image should fill without inline baseline gaps; missing %q in %s", check, slideImageBody)
		}
	}
	if !strings.Contains(slideImageBody, "inset: 0") {
		t.Errorf(".slide-image should pin every absolute slide to the wrapper; missing %q in %s", "inset: 0", slideImageBody)
	}
}

func TestShortsVideoPlaybackStartsImmediatelyWithPosterUntilFirstFrame(t *testing.T) {
	overlayBytes, err := os.ReadFile("../../static/js/src/shorts/overlay.js")
	if err != nil {
		t.Fatal(err)
	}
	itemsBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	overlaySrc := string(overlayBytes)
	itemsSrc := string(itemsBytes)
	css := string(cssBytes)

	for _, check := range []string{
		"function revealShortVideoIfReady(entry, video)",
		"function playShortVideo(entry, video)",
		"function playShortVideoFromStart(entry)",
		"try {\n    video.currentTime = 0",
		"playShortVideo(entry, video)",
	} {
		if !strings.Contains(overlaySrc, check) {
			t.Errorf("shorts overlay immediate playback missing %q", check)
		}
	}
	for _, forbidden := range []string{
		"function scheduleSettledShortPlayback(entry)",
		"pendingPlayTimer",
		"is-settling-playback",
	} {
		if strings.Contains(overlaySrc, forbidden) {
			t.Errorf("shorts overlay should not delay playback with %q", forbidden)
		}
	}
	if !strings.Contains(itemsSrc, "shorts-video-poster-frame") {
		t.Fatal("shorts video items should render a poster layer for first-frame fallback")
	}
	for _, check := range []string{
		"wrapper.classList.add('is-awaiting-first-frame')",
		"function revealVideoFrame()",
		"video.addEventListener('loadeddata', revealVideoFrame)",
		"video.addEventListener('playing', revealVideoFrame)",
	} {
		if !strings.Contains(itemsSrc, check) {
			t.Errorf("shorts video items first-frame fallback missing %q", check)
		}
	}
	if !strings.Contains(css, ".shorts-video-wrapper.is-awaiting-first-frame video") ||
		!strings.Contains(css, "opacity: 0;") {
		t.Fatal("first-frame fallback CSS should hide video over the stable poster")
	}
}

func TestShortsDebugToolsExposeOptInMediaSnapshots(t *testing.T) {
	debugBytes, err := os.ReadFile("../../static/js/src/shorts/debug.js")
	if err != nil {
		t.Fatal(err)
	}
	indexBytes, err := os.ReadFile("../../static/js/src/shorts/index.js")
	if err != nil {
		t.Fatal(err)
	}
	itemsBytes, err := os.ReadFile("../../static/js/src/shorts/items.js")
	if err != nil {
		t.Fatal(err)
	}
	overlayBytes, err := os.ReadFile("../../static/js/src/shorts/overlay.js")
	if err != nil {
		t.Fatal(err)
	}
	debugSrc := string(debugBytes)
	for _, check := range []string{
		"import { apiFetch } from '../utils.js'",
		"window.MpaShortsDebug",
		"shorts_debug=1",
		"shorts_debug=0",
		"localStorage.getItem('shortsDebug')",
		"_serverLog = '~/.local/share/igloo/logs/moments/debug.jsonl'",
		"event: 'moments_video_debug'",
		"flush: flush",
		"download: function ()",
		"status: function ()",
		"current: function ()",
		"recent: function ()",
		"copy: function ()",
		"function sampleBands(video)",
		"buffered: rangesOf(video.buffered)",
		"containerRect: rectOf(container)",
		"itemRect: rectOf(entry.el)",
		"snapDelta: snapDeltaOf(entry)",
		"visibleTopPx: visible.visibleTopPx",
		"visibleBottomPx: visible.visibleBottomPx",
		"visibleRatio: visible.visibleRatio",
		"visible: visible",
		"wrapperRect: rectOf(wrapper)",
		"videoRect: rectOf(video)",
		"infoRect: rectOf(info)",
		"authorRect: rectOf(author)",
		"titleRect: rectOf(title)",
		"actionsRect: rectOf(actions)",
		"progressRect: rectOf(progress)",
		"chrome: chromeSnapshot(entry)",
		"isSkeletonCard: !!(entry.data && entry.data.isSkeleton)",
		"containerScroll: container ?",
		"wrapperRadius: wrapperStyle && wrapperStyle.borderRadius",
		"videoDisplay: videoStyle && videoStyle.display",
		"requestVideoFrameCallback",
	} {
		if !strings.Contains(debugSrc, check) {
			t.Errorf("shorts debug tool missing %q", check)
		}
	}
	if !strings.Contains(string(indexBytes), "initShortsDebug(state)") {
		t.Fatal("shorts debug should initialize with player state")
	}
	if !strings.Contains(string(itemsBytes), "attachShortVideoDebug(entryObj)") {
		t.Fatal("shorts items should attach video event diagnostics")
	}
	for _, check := range []string{
		"recordShortsDebugEvent(entry, 'intersect:candidate'",
		"recordShortsDebugEvent(entry, 'activate'",
		"recordShortsDebugEvent(entry, 'play:attempt')",
		"recordShortsDebugEvent(entry, 'chrome:snapshot'",
		"recordShortsDebugEvent(target, 'deck:transition-start'",
	} {
		if !strings.Contains(string(overlayBytes), check) {
			t.Errorf("shorts overlay debug event missing %q", check)
		}
	}
}
