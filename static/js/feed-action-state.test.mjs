import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

function extractFunction(source, name) {
  var start = source.indexOf('function ' + name + '(')
  assert.notEqual(start, -1, 'missing ' + name)
  var bodyStart = source.indexOf('{', start)
  var depth = 0
  for (var i = bodyStart; i < source.length; i++) {
    if (source[i] === '{') depth++
    if (source[i] === '}') depth--
    if (depth === 0) return source.slice(start, i + 1)
  }
  throw new Error('unterminated ' + name)
}

class FakeCard {
  constructor(tweetId, contentHash, bookmarked) {
    this.attributes = new Map([
      ['data-tweet-id', tweetId],
      ['data-content-hash', contentHash],
    ])
    this.dataset = { liked: '1', bookmarked: bookmarked ? '1' : '0' }
  }

  getAttribute(name) { return this.attributes.get(name) || null }
}

test('bookmark state reaches another projection of the same post', async () => {
  var source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
  var root = new FakeCard('same_post', 'root_hash', true)
  var threadCopy = new FakeCard('same_post', 'thread_hash', false)
  var relatedCopy = new FakeCard('related_post', 'root_hash', false)
  var cards = [root, threadCopy, relatedCopy]
  var context = vm.createContext({
    document: {
      querySelectorAll(selector) {
        return cards.filter(function (card) {
          return selector.includes('data-tweet-id="' + card.getAttribute('data-tweet-id') + '"') ||
            selector.includes('data-content-hash="' + card.getAttribute('data-content-hash') + '"')
        })
      },
    },
    cssEscape: (value) => value,
    stateBool: (card, key) => card.dataset[key] === '1',
    setStateBool: (card, key, value) => { card.dataset[key] = value ? '1' : '0' },
    syncFeedButtons: () => {},
  })
  vm.runInContext(extractFunction(source, 'syncSiblingCards') + '\nthis.syncSiblingCards = syncSiblingCards', context)

  context.syncSiblingCards(root)

  assert.equal(threadCopy.dataset.bookmarked, '1')
  assert.equal(relatedCopy.dataset.bookmarked, '1')
})

test('double-clicking a playing feed video opens its overlay still playing', async () => {
  var source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
  var opened = false
  var pendingClick = null
  var video = {
    muted: false,
    paused: false,
    playCount: 0,
    play() {
      this.paused = false
      this.playCount++
      return Promise.resolve()
    },
    pause() { this.paused = true },
  }
  var mediaTrigger = {
    querySelector(selector) {
      assert.equal(selector, 'video')
      return video
    },
    setAttribute() {},
  }
  var context = vm.createContext({
    window: {
      setTimeout(callback, delay) {
        assert.equal(delay, 250)
        pendingClick = callback
        return 1
      },
      clearTimeout() { pendingClick = null },
    },
    openMediaOverlay(root, trigger) {
      assert.equal(root, mediaTrigger)
      assert.equal(trigger, mediaTrigger)
      opened = true
    },
    exitFeedVideoFullscreen() { return false },
  })
  vm.runInContext(
    extractFunction(source, 'clearPendingInlineVideoClick') + '\n' +
      extractFunction(source, 'handleInlineVideoClick') +
      '\nthis.handleInlineVideoClick = handleInlineVideoClick',
    context,
  )

  context.handleInlineVideoClick(mediaTrigger, { detail: 1 })
  assert.equal(video.paused, false)
  assert.ok(pendingClick)

  context.handleInlineVideoClick(mediaTrigger, { detail: 2 })
  assert.equal(opened, true)
  assert.equal(video.paused, false)
  assert.equal(video.playCount, 0)
  assert.equal(pendingClick, null)
})

test('double-clicking a fullscreen feed video exits without opening the overlay', async () => {
  var source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
  var exitedVideo = null
  var opened = false
  var pendingClick = true
  var video = { muted: false, paused: false }
  var mediaTrigger = {
    querySelector(selector) {
      assert.equal(selector, 'video')
      return video
    },
  }
  var context = vm.createContext({
    window: { clearTimeout() { pendingClick = null } },
    exitFeedVideoFullscreen(candidate) {
      exitedVideo = candidate
      return true
    },
    openMediaOverlay() { opened = true },
  })
  vm.runInContext(
    extractFunction(source, 'clearPendingInlineVideoClick') + '\n' +
      extractFunction(source, 'handleInlineVideoClick') +
      '\nthis.handleInlineVideoClick = handleInlineVideoClick',
    context,
  )
  mediaTrigger._feedVideoClickTimer = 1

  context.handleInlineVideoClick(mediaTrigger, { detail: 2 })

  assert.equal(exitedVideo, video)
  assert.equal(opened, false)
  assert.equal(pendingClick, null)
})

test('fullscreen keyboard ownership recognizes the video element itself', async () => {
  var source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
  var card = {}
  var video = {
    tagName: 'VIDEO',
    closest(selector) {
      assert.equal(selector, '[data-feed-item]')
      return card
    },
  }
  var context = vm.createContext({
    document: { fullscreenElement: video },
  })
  vm.runInContext(
    extractFunction(source, 'fullscreenFeedSurface') +
      '\nthis.fullscreenFeedSurface = fullscreenFeedSurface',
    context,
  )

  var surface = context.fullscreenFeedSurface()
  assert.equal(surface.video, video)
  assert.equal(surface.card, card)
})

test('single-clicking a playing feed video pauses after the double-click window', async () => {
  var source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
  var pendingClick = null
  var video = {
    muted: false,
    paused: false,
    play() { return Promise.resolve() },
    pause() { this.paused = true },
  }
  var mediaTrigger = {
    querySelector() { return video },
    setAttribute() {},
  }
  var context = vm.createContext({
    window: {
      setTimeout(callback) {
        pendingClick = callback
        return 1
      },
      clearTimeout() { pendingClick = null },
    },
    openMediaOverlay() {},
  })
  vm.runInContext(
    extractFunction(source, 'clearPendingInlineVideoClick') + '\n' +
      extractFunction(source, 'handleInlineVideoClick') +
      '\nthis.handleInlineVideoClick = handleInlineVideoClick',
    context,
  )

  context.handleInlineVideoClick(mediaTrigger, { detail: 1 })
  assert.equal(video.paused, false)
  pendingClick()
  assert.equal(video.paused, true)
})
