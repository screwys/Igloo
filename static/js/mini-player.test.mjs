import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  feedAccountName,
  feedThreadURL,
  findFeedThreadVideo,
  preferredShellWidth,
  reconnectMediaController,
  resizedShellWidth,
  shouldDismissForYouTube,
  shouldAutomaticallyMini,
} from './src/mini-player.js'

test('the manual player button is independent from automatic preferences', () => {
  const preferences = {
    miniPlayerVideosEnabled: false,
    miniPlayerFeedEnabled: false,
  }
  assert.equal(shouldAutomaticallyMini('videos', preferences), false)
  assert.equal(shouldAutomaticallyMini('feed', preferences), false)
})

test('automatic mini-player preferences independently control Videos and Feed', () => {
  const preferences = { miniPlayerVideosEnabled: true, miniPlayerFeedEnabled: false }
  assert.equal(shouldAutomaticallyMini('videos', preferences), true)
  assert.equal(shouldAutomaticallyMini('feed', preferences), false)
  assert.equal(shouldAutomaticallyMini('videos', { miniPlayerVideosEnabled: false }), false)
  assert.equal(shouldAutomaticallyMini('feed', { miniPlayerFeedEnabled: true }), true)
})

test('manual docking and closing do not navigate or reload the page', async () => {
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /enterMini\(preferredManualDestination/)
  assert.doesNotMatch(source, /window\.location\.assign\(destination\)/)
  assert.match(source, /function returnToSurface[\s\S]*setAppBrowsing\(false\)[\s\S]*restoreActiveSurface/)
  assert.match(source, /app\.removeAttribute\('inert'\)/)
  assert.match(source, /returnButton\.addEventListener\('click', returnToSurface\)/)
  assert.match(source, /closeButton\.addEventListener\('click', closeMiniPlayer\)/)
})

test('the mini-player defaults to one and a half times its previous width', async () => {
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8')
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  assert.match(css, /width:\s*clamp\(480px, 42vw, 630px\)/)
  assert.match(css, /max-width:\s*min\(780px, calc\(100vw - 2rem\)\)/)
  assert.match(source, /igloo\.mini-player\.width\.v2/)
  assert.match(css, /media-volume-range\s*\+\s*\.mc-custom-btn/)
})

test('saved mini-player width migrates once and then remains exact', () => {
  assert.equal(preferredShellWidth(0, 420, 1400), 630)
  assert.equal(preferredShellWidth(555, 420, 1400), 555)
  assert.equal(preferredShellWidth(0, 0, 1400), 0)
})

test('mini-player width remains horizontally resizable and persisted', async () => {
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8')
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  const template = await readFile(new URL('../../internal/components/mini_player.templ', import.meta.url), 'utf8')
  assert.doesNotMatch(css, /resize:\s*horizontal/)
  assert.match(template, /id="mini-player-resize-handle"[\s\S]*?role="separator"/)
  for (const corner of ['top-left', 'top-right', 'bottom-left', 'bottom-right']) {
    assert.match(template, new RegExp(`data-mini-player-resize="${corner}"`))
  }
  const handleRule = css.match(/\.mini-player-resize-handle\s*\{([^}]*)\}/)
  assert.ok(handleRule)
  assert.match(handleRule[1], /\bleft:\s*-8px;/)
  assert.match(handleRule[1], /\btop:\s*0;/)
  assert.match(handleRule[1], /\bbottom:\s*0;/)
  assert.match(handleRule[1], /\bwidth:\s*9px;/)
  assert.match(handleRule[1], /\bcursor:\s*(?:ew|col)-resize/)
  assert.doesNotMatch(handleRule[1], /\bheight:/)
  assert.doesNotMatch(handleRule[1], /\btransform:/)
  assert.doesNotMatch(css, /\.mini-player-resize-handle::after/)
  const shellRule = css.match(/\.mini-player-shell\s*\{([^}]*)\}/)
  assert.ok(shellRule)
  assert.match(shellRule[1], /\boverflow:\s*visible;/)
  const activeEdgeRule = css.match(/\.mini-player-shell:has\(\.mini-player-resize-target:hover\),[\s\S]*?html\.mini-player-resizing \.mini-player-shell\s*\{([^}]*)\}/)
  assert.ok(activeEdgeRule)
  assert.match(activeEdgeRule[1], /\bborder-color:\s*rgba\(var\(--accent-primary-rgb\)/)
  assert.match(activeEdgeRule[1], /\bbox-shadow:/)
  assert.match(css, /\.mini-player-media-host\s*\{[^}]*\boverflow:\s*hidden;/)
  assert.match(source, /target\.setPointerCapture\(event\.pointerId\)/)
  assert.match(source, /new window\.ResizeObserver[\s\S]*?localStorage\.setItem\(SHELL_WIDTH_KEY, String\(next\)\)/)
})

test('each rounded corner resizes in its natural diagonal direction', () => {
  assert.equal(resizedShellWidth('top-left', 600, 300, -40, -20), 640)
  assert.equal(resizedShellWidth('top-right', 600, 300, 40, -20), 640)
  assert.equal(resizedShellWidth('bottom-left', 600, 300, -40, 20), 640)
  assert.equal(resizedShellWidth('bottom-right', 600, 300, 40, 20), 640)
  assert.equal(resizedShellWidth('bottom-right', 600, 300, -40, -20), 560)
})

test('mini-player title and actions overlay the video on hover', async () => {
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8')
  assert.match(css, /\.mini-player-header\s*\{[\s\S]*?position:\s*absolute/)
  assert.match(css, /\.mini-player-shell:hover\s+\.mini-player-header/)
  assert.match(css, /\.mini-player-shell:focus-within\s+\.mini-player-header/)
})

test('moving the player reconnects Media Chrome request handling', async () => {
  const calls = []
  const controller = { associateElement(element) { calls.push(element) } }
  const surface = { querySelector(selector) { return selector === 'media-controller' ? controller : null } }
  assert.equal(reconnectMediaController(surface), true)
  assert.deepEqual(calls, [controller])
  assert.equal(reconnectMediaController({ querySelector() { return null } }), false)

  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  assert.match(source, /mediaHost\.appendChild\(next\.element\)[\s\S]*?reconnectMediaController\(next\.element\)/)
  assert.match(source, /placeholder\.replaceWith\(surface\.element\)[\s\S]*?reconnectMediaController\(surface\.element\)/)
})

test('navigation retains a browse frame that owns the live mini-player source', async () => {
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8')
  assert.match(source, /function retainActiveSourceFrame\(\)/)
  assert.match(source, /activeSurface\.sourceDocument !== browseFrame\.contentDocument/)
  assert.match(source, /activeSurface\.sourceFrame = retained/)
  assert.match(source, /function navigateBrowse[\s\S]*?retainActiveSourceFrame\(\)/)
  assert.match(source, /function activateSourceFrame\(/)
  assert.match(css, /\.mini-player-source-frame\s*\{[\s\S]*?pointer-events:\s*none/)
})

test('feed mini-player titles use the visible account name', () => {
  const inlineAuthor = { textContent: '  Sample account  ' }
  const overlayAuthor = { textContent: '  Overlay account  ' }
  const inlineCard = { querySelector() { return inlineAuthor } }
  const overlay = { querySelector() { return overlayAuthor } }
  const inlineWrap = {
    closest(selector) { return selector === '[data-feed-item]' ? inlineCard : null },
  }
  const overlayWrap = {
    closest(selector) { return selector === '.feed-media-overlay' ? overlay : null },
  }
  assert.equal(feedAccountName(inlineWrap), 'Sample account')
  assert.equal(feedAccountName(overlayWrap), 'Overlay account')
})

test('X mini-player return destinations use the local thread route', () => {
  assert.equal(feedThreadURL('post_1'), '/thread/post_1')
  assert.equal(feedThreadURL('post/with spaces'), '/thread/post%2Fwith%20spaces')
  assert.equal(feedThreadURL(''), '')
})

test('X return finds the same video in the thread before moving the live element', () => {
  function media(stream) {
    return {
      getAttribute(name) { return name === 'data-feed-media-stream' ? stream : '' },
      querySelector(selector) { return selector === 'video' ? {} : null },
    }
  }
  function card(tweetID, videos) {
    return {
      getAttribute(name) { return name === 'data-tweet-id' ? tweetID : '' },
      querySelectorAll() { return videos },
    }
  }
  const first = media('/media/first')
  const matching = media('/media/matching')
  const ownerDocument = {
    querySelectorAll() {
      return [card('other', [first]), card('post_1', [first, matching])]
    },
  }
  assert.equal(findFeedThreadVideo(ownerDocument, 'post_1', 'https://localhost:8443/media/matching'), matching)
  assert.equal(findFeedThreadVideo(ownerDocument, 'missing', '/media/matching'), null)
})

test('a different YouTube player dismisses an existing YouTube mini player', () => {
  const current = {}
  const next = {}
  assert.equal(shouldDismissForYouTube('videos', current, next), true)
  assert.equal(shouldDismissForYouTube('videos', current, current), false)
  assert.equal(shouldDismissForYouTube('feed', current, next), false)
})
