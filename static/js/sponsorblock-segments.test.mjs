import assert from 'node:assert/strict'
import test from 'node:test'

import { sponsorSegmentLayout } from './src/player/sponsorblock-segments.js'

test('SponsorBlock segments keep fixed full-timeline positions after playback passes them', () => {
  const layout = sponsorSegmentLayout([
    { start: 40, end: 60, category: 'sponsor' },
    { start: 10, end: 20, category: 'intro' },
  ], 100, { sponsor: '#0f0', intro: '#0ff' })

  assert.deepEqual(layout, [
    { left: 10, width: 10, color: '#0ff' },
    { left: 40, width: 20, color: '#0f0' },
  ])
})
