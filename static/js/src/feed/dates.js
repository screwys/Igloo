// Dates module — extracted from feed_page.js
// Hydrates relative dates on feed cards.

import { formatRelative } from '../utils.js'

export function initDates(container) {
  const scope = container || document
  scope.querySelectorAll('.feed-date-inline[data-feed-date-raw], .feed-quote-date[data-feed-date-raw]').forEach(function (el) {
    if (!el) return
    const raw = String(el.getAttribute('data-feed-date-raw') || '').trim()
    if (!raw) return
    const rel = formatRelative(raw)
    el.setAttribute('data-date-relative', rel)
    el.textContent = rel || raw
  })
}

document.addEventListener('igloo:i18n:changed', function () {
  initDates(document)
})

window.FeedDates = { init: initDates }
