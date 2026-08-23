import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

async function loadShareHelpers(preferences) {
  const source = await readFile(new URL('./src/utils.js', import.meta.url), 'utf8')
  const runnable = source.replace(/\bexport\s+/g, '') +
    '\nObject.assign(globalThis, { toFxTwitterUrl });'
  const context = vm.createContext({
    URL,
    window: {
      location: { origin: 'https://localhost:8443' },
      IglooPreferences: preferences,
    },
    document: { querySelector() { return null } },
  })
  vm.runInContext(runnable, context, { filename: 'utils.js' })
  return context
}

test('embed-friendly share hosts are configurable per platform', async () => {
  const helpers = await loadShareHelpers({
    shareEmbedFriendlyLinks: true,
    shareEmbedHosts: {
      twitter: 'fixupx.example:8443',
      tiktok: 'https://tok.example/path-is-ignored',
      instagram: '',
      youtube: 'yt.example',
    },
  })

  assert.equal(helpers.toFxTwitterUrl('https://x.com/sample/status/1'), 'https://fixupx.example:8443/sample/status/1')
  assert.equal(helpers.toFxTwitterUrl('https://www.tiktok.com/@sample/video/2'), 'https://tok.example/@sample/video/2')
  assert.equal(helpers.toFxTwitterUrl('https://instagram.com/p/3'), 'https://instagram.com/p/3')
  assert.equal(helpers.toFxTwitterUrl('https://youtube.com/watch?v=4'), 'https://yt.example/watch?v=4')
})

test('embed-friendly share hosts keep their existing defaults', async () => {
  const helpers = await loadShareHelpers({ shareEmbedFriendlyLinks: true })

  assert.equal(helpers.toFxTwitterUrl('https://twitter.com/sample/status/1'), 'https://fxtwitter.com/sample/status/1')
  assert.equal(helpers.toFxTwitterUrl('https://tiktok.com/@sample/video/2'), 'https://tnktok.com/@sample/video/2')
  assert.equal(helpers.toFxTwitterUrl('https://instagram.com/p/3'), 'https://vxinstagram.com/p/3')
  assert.equal(helpers.toFxTwitterUrl('https://youtube.com/watch?v=4'), 'https://youtube.com/watch?v=4')
})
