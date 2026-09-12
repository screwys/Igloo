// Dates module — extracted from feed_page.js
// Hydrates relative/absolute dates on feed cards.

import { formatRelative, formatAbsolute } from '../utils.js'

export function initDates(container) {
  const scope = container || document
  scope.querySelectorAll('.feed-date-inline[data-feed-date-raw], .feed-quote-date[data-feed-date-raw]').forEach(function (el) {
    if (!el) return
    const raw = String(el.getAttribute('data-feed-date-raw') || '').trim()
    if (!raw) return
    const rel = formatRelative(raw)
    const abs = formatAbsolute(raw)
    el.setAttribute('data-date-relative', rel)
    el.setAttribute('data-date-absolute', abs)
    el.textContent = rel || raw
    el.title = abs || raw
  })
}

document.addEventListener('igloo:i18n:changed', function () {
  initDates(document)
})

window.FeedDates = { init: initDates }
