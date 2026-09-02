export function sponsorSegmentLayout(segments, duration, colors) {
  const dur = Number(duration)
  if (!(dur > 0) || !Array.isArray(segments)) return []
  return segments.slice().sort(function (a, b) {
    return Number(a.start) - Number(b.start)
  }).map(function (segment) {
    const start = Math.max(0, Math.min(dur, Number(segment.start) || 0))
    const end = Math.max(start, Math.min(dur, Number(segment.end) || 0))
    return {
      left: start / dur * 100,
      width: (end - start) / dur * 100,
      color: colors[segment.category] || '#888',
    }
  }).filter(function (segment) {
    return segment.width > 0
  })
}
