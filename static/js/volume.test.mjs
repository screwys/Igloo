import assert from 'node:assert/strict'
import test from 'node:test'

import {
  normalizeVolume,
  readStoredVolume,
  volumeIconLevel,
  writeStoredVolume,
} from './src/volume.js'

test('volume preferences clamp invalid and out-of-range values', () => {
  assert.equal(normalizeVolume(-0.5, 1), 0)
  assert.equal(normalizeVolume(1.5, 1), 1)
  assert.equal(normalizeVolume('0.35', 1), 0.35)
  assert.equal(normalizeVolume('invalid', 0.6), 0.6)
})

test('volume preferences persist independently by storage key', () => {
  const values = new Map()
  const storage = {
    getItem(key) { return values.has(key) ? values.get(key) : null },
    setItem(key, value) { values.set(key, value) },
  }

  writeStoredVolume(storage, 'feedVolume', 0.25)
  writeStoredVolume(storage, 'youtubeVolume', 0.8)

  assert.equal(readStoredVolume(storage, 'feedVolume', 1), 0.25)
  assert.equal(readStoredVolume(storage, 'youtubeVolume', 1), 0.8)
  assert.equal(readStoredVolume(storage, 'shortsVolume', 0.6), 0.6)
})

test('volume icons distinguish muted, low, and high output', () => {
  assert.equal(volumeIconLevel(true, 0.8), 'muted')
  assert.equal(volumeIconLevel(false, 0), 'muted')
  assert.equal(volumeIconLevel(false, 0.35), 'low')
  assert.equal(volumeIconLevel(false, 0.5), 'high')
})
