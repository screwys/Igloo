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
