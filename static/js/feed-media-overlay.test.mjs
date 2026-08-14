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
    '\nObject.assign(globalThis, { handoffInlineVideoPlayback });'

  const document = {
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
  return {
    currentTime,
    pauseCount: 0,
    closest(selector) {
      assert.equal(selector, '[data-feed-media]')
      return wrap
    },
    pause() {
      this.pauseCount += 1
    },
  }
}

test('video overlay pauses every inline video and takes over the selected stream', async () => {
  const selected = inlineVideo('/api/media/stream/sample_new', 42)
  const quoted = inlineVideo('/api/media/stream/sample_quote', 17)
  const media = await loadMediaOverlay([selected, quoted])
  const overlay = {}
  const overlayVideo = {
    currentTime: 0,
    addEventListener(name, listener, options) {
      assert.equal(name, 'loadedmetadata')
      assert.equal(options.once, true)
      listener()
    },
  }

  media.handoffInlineVideoPlayback(
    overlay,
    overlayVideo,
    '/api/media/stream/sample_new',
  )

  assert.equal(selected.pauseCount, 1)
  assert.equal(quoted.pauseCount, 1)
  assert.equal(overlay._sourceVideo, selected)
  assert.equal(overlay._overlayVideo, overlayVideo)
  assert.equal(overlayVideo.currentTime, 42)
})
