import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

async function loadMediaOverlay(inlineVideos) {
  const source = await readFile(new URL('./src/feed/media-overlay.js', import.meta.url), 'utf8')
  const runnable = source
    .replace(/import \{[\s\S]*?\} from '\.\.\/utils\.js'\n/, '')
    .replace(/^import .*$/gm, '')
    .replace(/\bexport\s+/g, '') +
    '\nObject.assign(globalThis, { takeOverInlineVideoPlayback, restoreInlineVideoPlayback });'

  const document = {
    createElement(tagName) {
      assert.equal(tagName, 'div')
      return { parentNode: null, style: {} }
    },
    querySelectorAll(selector) {
      assert.equal(selector, 'video[data-feed-inline-video]')
      return inlineVideos
    },
  }
  const context = vm.createContext({ document, window: {} })
  vm.runInContext(runnable, context, { filename: 'media-overlay.js' })
  return context
}

function inlineVideo(streamUrl, currentTime) {
  const wrap = {
    getAttribute(name) {
      return name === 'data-feed-media-stream' ? streamUrl : null
    },
  }
  const parent = {
    child: null,
    replaceChild(next, previous) {
      assert.equal(previous, this.child)
      this.child = next
      next.parentNode = this
      previous.parentNode = null
      return previous
    },
  }
  const attributes = new Map()
  const classes = new Set(['feed-media-video'])
  const listeners = new Map()
  const video = {
    className: 'feed-media-video',
    currentTime,
    muted: false,
    pauseCount: 0,
    parentNode: parent,
    classList: {
      add(name) {
        classes.add(name)
        video.className = Array.from(classes).join(' ')
      },
    },
    setAttribute(name, value) { attributes.set(name, String(value)) },
    removeAttribute(name) { attributes.delete(name) },
    getBoundingClientRect() { return { width: 640, height: 360 } },
    addEventListener(name, listener) { listeners.set(name, listener) },
    removeEventListener(name, listener) {
      if (listeners.get(name) === listener) listeners.delete(name)
    },
    closest(selector) {
      assert.equal(selector, '[data-feed-media]')
      return wrap
    },
    pause() {
      this.pauseCount += 1
    },
  }
  parent.child = video
  return video
}

test('video overlay moves the selected player without interrupting playback', async () => {
  const selected = inlineVideo('/api/media/stream/sample_new', 42)
  const quoted = inlineVideo('/api/media/stream/sample_quote', 17)
  const media = await loadMediaOverlay([selected, quoted])
  const overlay = {}

  const overlayVideo = media.takeOverInlineVideoPlayback(
    overlay,
    '/api/media/stream/sample_new',
  )

  assert.equal(overlayVideo, selected)
  assert.equal(selected.pauseCount, 0)
  assert.equal(quoted.pauseCount, 1)
  assert.equal(overlay._sourceVideo, selected)
  assert.equal(selected.currentTime, 42)
  assert.equal(selected.parentNode, null)
  assert.equal(overlay._sourcePlaceholder.parentNode.child, overlay._sourcePlaceholder)
  assert.match(selected.className, /feed-overlay-video/)

  selected.currentTime = 57
  media.restoreInlineVideoPlayback(overlay, true)

  assert.equal(selected.currentTime, 57)
  assert.equal(selected.pauseCount, 0)
  assert.equal(selected.parentNode.child, selected)
  assert.equal(selected.className, 'feed-media-video')
})
