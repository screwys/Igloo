import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  canAutomaticallyDockVideo,
  feedAccountName,
  feedThreadURL,
  findFeedThreadVideo,
  isPlayerURL,
  movedShellPosition,
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

test('automatic handoff only moves a currently playing video', () => {
  assert.equal(canAutomaticallyDockVideo({ ended: true, paused: true, currentTime: 600 }), false)
  assert.equal(canAutomaticallyDockVideo({ ended: false, paused: true, currentTime: 90 }), false)
  assert.equal(canAutomaticallyDockVideo({ ended: false, paused: false, currentTime: 0 }), true)
})

test('a player route is opened as the new full player, not inside the mini player', () => {
  assert.equal(isPlayerURL('/player/next-video?autoplay=1'), true)
  assert.equal(isPlayerURL('/videos'), false)
  assert.equal(isPlayerURL('/player/next/video'), false)
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
  assert.match(css, /max-width:\s*min\(1040px, calc\(100vw - 2rem\)\)/)
  assert.match(source, /igloo\.mini-player\.width\.v2/)
  assert.match(css, /media-volume-range\s*\+\s*\.mc-custom-btn/)
})

test('saved mini-player width migrates once and then remains exact', () => {
  assert.equal(preferredShellWidth(0, 420, 1400), 630)
  assert.equal(preferredShellWidth(555, 420, 1400), 555)
  assert.equal(preferredShellWidth(1000, 420, 1400), 1000)
  assert.equal(preferredShellWidth(0, 0, 1400), 0)
})

test('mini-player width remains horizontally resizable and persisted', async () => {
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8')
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  const template = await readFile(new URL('../../internal/components/mini_player.templ', import.meta.url), 'utf8')
  assert.doesNotMatch(css, /resize:\s*horizontal/)
  assert.match(template, /id="mini-player-resize-handle"[\s\S]*?role="separator"/)
  assert.match(template, /aria-valuemax="1040"/)
  assert.match(template, /id="mini-player-move-handle"[\s\S]*?class="mini-player-move-handle"/)
  assert.doesNotMatch(template, /data-mini-player-resize="top"/)
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
  const moveHandleRule = css.match(/\.mini-player-move-handle\s*\{([^}]*)\}/)
  assert.ok(moveHandleRule)
  assert.match(moveHandleRule[1], /\btop:\s*-3px;/)
  assert.match(moveHandleRule[1], /\bleft:\s*16px;/)
  assert.match(moveHandleRule[1], /\bright:\s*16px;/)
  assert.match(moveHandleRule[1], /\bheight:\s*8px;/)
  assert.match(moveHandleRule[1], /\bcursor:\s*grab/)
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

test('moving the mini-player follows the pointer without leaving the viewport', () => {
  assert.deepEqual(movedShellPosition(700, 400, 500, 300, -180, -120, 1280, 720), { left: 520, top: 280 })
  assert.deepEqual(movedShellPosition(700, 400, 500, 300, 300, 200, 1280, 720), { left: 772, top: 412 })
  assert.deepEqual(movedShellPosition(20, 20, 500, 300, -100, -100, 1280, 720), { left: 8, top: 8 })
})

test('resizing from a right corner keeps the left edge fixed', async () => {
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  assert.match(source, /const left = direction\.includes\('left'\) \? startRect\.right - nextRect\.width : startRect\.left/)
  assert.doesNotMatch(source, /shell\.style\.right = Math\.round\(window\.innerWidth - finalRect\.right\)/)
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

test('page-owned link interactions run before mini-player navigation', async () => {
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  assert.match(source, /if \(event\.defaultPrevented \|\| event\.button !== 0\) return null/)
  const frameClickStart = source.indexOf("frameDocument.addEventListener('click'")
  const frameClickEnd = source.indexOf("frameDocument.addEventListener('submit'", frameClickStart)
  const pageClickStart = source.indexOf("doc.addEventListener('click'")
  const pageClickEnd = source.indexOf("if (returnButton)", pageClickStart)
  assert.ok(frameClickStart >= 0 && frameClickEnd > frameClickStart)
  assert.ok(pageClickStart >= 0 && pageClickEnd > pageClickStart)
  assert.doesNotMatch(source.slice(frameClickStart, frameClickEnd), /\}, true\)/)
  assert.doesNotMatch(source.slice(pageClickStart, pageClickEnd), /\}, true\)/)
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

test('a clicked player route closes the current mini player before navigation', async () => {
  const source = await readFile(new URL('./src/mini-player.js', import.meta.url), 'utf8')
  assert.match(source, /function leaveMiniPlayerForPlayer\(value\)[\s\S]*?restoreActiveSurface\(\{ pause: true \}\)[\s\S]*?window\.location\.assign\(value\)/)
  assert.match(source, /activeSurface\.kind === 'videos' && isPlayerURL\(target\.href, frameWindow\.location\.href\)[\s\S]*?leaveMiniPlayerForPlayer\(target\.href\)/)
})
