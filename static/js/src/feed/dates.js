// Dates module — extracted from feed_page.js
// Hydrates relative dates on feed cards.

import { formatRelative, parseAppDate } from '../utils.js'

export function formatFullDate(raw) {
  const date = parseAppDate(raw)
  if (!date) return ''
  const locale = document.documentElement.lang || undefined
  return date.toLocaleTimeString(locale, { hour: 'numeric', minute: '2-digit' }) + ' · ' +
    date.toLocaleDateString(locale, { month: 'short', day: 'numeric', year: 'numeric' })
}

export function initDates(container) {
  const scope = container || document
  scope.querySelectorAll('.feed-date-inline[data-feed-date-raw], .feed-quote-date[data-feed-date-raw]').forEach(function (el) {
    if (!el) return
    const raw = String(el.getAttribute('data-feed-date-raw') || '').trim()
    if (!raw) return
    if (el.getAttribute('data-date-format') === 'full') {
      el.textContent = formatFullDate(raw) || raw
    } else {
      const rel = formatRelative(raw)
      el.setAttribute('data-date-relative', rel)
      el.textContent = rel || raw
    }
  })
}

document.addEventListener('igloo:i18n:changed', function () {
  initDates(document)
})

window.FeedDates = { init: initDates }
