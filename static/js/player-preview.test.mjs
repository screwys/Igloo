import assert from 'node:assert/strict'
import test from 'node:test'

import { findPreviewSegmentAtTime } from './src/player/preview.js'

test('preview uses the most specific SponsorBlock label at the hovered time', () => {
  const segments = [
    { start: 0, end: 20, label: 'Sponsor' },
    { start: 5, end: 10, label: 'Interaction reminder' },
  ]

  assert.equal(findPreviewSegmentAtTime(segments, 7).label, 'Interaction reminder')
  assert.equal(findPreviewSegmentAtTime(segments, 15).label, 'Sponsor')
  assert.equal(findPreviewSegmentAtTime(segments, 20), null)
})
