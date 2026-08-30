import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const playerIndex = readFileSync(new URL('./src/player/index.js', import.meta.url), 'utf8')

test('YouTube unsubscribe returns to the Videos page', () => {
  assert.match(
    playerIndex,
    /window\.location\.assign\(channelPlatform === 'youtube' \? '\/videos' : '\/channels'\)/,
  )
})

test('player bookmarks use the video title as the default label', () => {
  assert.match(
    playerIndex,
    /titleFallback:\s*String\(\(playerTitle && playerTitle\.textContent\) \|\| ''\)\.trim\(\)/,
  )
  assert.match(playerIndex, /bodyText:\s*desc/)
})
