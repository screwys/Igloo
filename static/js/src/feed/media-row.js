import { materialIconMarkup, t } from '../utils.js'

// Post media that fits the post width sits side by side in one row. Larger sets
// show one image at a time with the next peeking in, and can be fitted side by side.

function rowTiles(root) {
  return [...root.querySelector('.feed-media-grid').children]
}

// Each step focuses the neighbouring image at the left edge. Going back from a
// partly scrolled image first realigns that image.
function stepTarget(viewport, tiles, from, direction) {
  const end = Math.max(0, viewport.scrollWidth - viewport.clientWidth)
  const current = Math.max(0, tiles.findLastIndex(tile => tile.offsetLeft <= from + 1))
  const index = direction < 0 && from > tiles[current].offsetLeft + 1 ? current : current + direction
  return Math.min(end, tiles[Math.max(0, index)]?.offsetLeft ?? end)
}

function updateNavigation(root) {
  const viewport = root.querySelector('[data-feed-media-scroll]')
  const overflow = viewport.scrollWidth > viewport.clientWidth + 1
  root.querySelectorAll('[data-feed-media-step]').forEach(button => {
    button.hidden = !overflow
    button.disabled = Number(button.dataset.feedMediaStep) < 0
      ? viewport.scrollLeft <= 1
      : viewport.scrollLeft + viewport.clientWidth >= viewport.scrollWidth - 1
  })
  const counter = root.querySelector('[data-feed-media-counter]')
  counter.hidden = !overflow
  if (overflow) {
    const tiles = rowTiles(root)
    const index = tiles.findIndex(tile => tile.offsetLeft + tile.offsetWidth / 2 > viewport.scrollLeft)
    counter.textContent = `${index < 0 ? tiles.length : index + 1} / ${tiles.length}`
  }
}

function layoutMediaRow(root) {
  const row = root.querySelector('.feed-media-row')
  const viewport = root.querySelector('[data-feed-media-scroll]')
  if (row) {
    root.removeAttribute('data-feed-media-carousel')
    row.style.removeProperty('--feed-row-height')
    const width = row.getBoundingClientRect().width
    const overflow = width - viewport.clientWidth
    const small = overflow <= width * 0.15
    // Fit small remainders, or any row the reader fitted, without cropping or changing image proportions.
    if (overflow > 0 && (small || root.hasAttribute('data-feed-media-fitted'))) {
      row.style.setProperty('--feed-row-height', `calc((${viewport.clientWidth}px - var(--feed-row-gaps)) / var(--feed-row-ratio))`)
    } else if (!small) {
      root.setAttribute('data-feed-media-carousel', '')
      row.style.setProperty('--feed-slot-width', `${Math.max(1, viewport.clientWidth - 48)}px`)
    }
    root.querySelector('[data-feed-media-fit-toggle]').hidden = small
  }
  updateNavigation(root)
}

function syncFitToggle(root) {
  const button = root.querySelector('[data-feed-media-fit-toggle]')
  const fitted = root.hasAttribute('data-feed-media-fitted')
  button.title = fitted ? t('feed_media_show_one', 'Show one at a time') : t('feed_media_show_all', 'Show all side by side')
  button.setAttribute('aria-label', button.title)
  button.innerHTML = materialIconMarkup(fitted ? 'ZoomOutMap' : 'ZoomInMap') // eslint-disable-line no-unsanitized/property
}

export function initMediaRows(scope) {
  scope.querySelectorAll('.feed-media-row-container').forEach(root => {
    if (root.dataset.mediaRowReady) return
    root.dataset.mediaRowReady = '1'
    const viewport = root.querySelector('[data-feed-media-scroll]')
    // Repeated steps continue from the pending destination, not a midway position.
    let target = null
    viewport.addEventListener('scrollend', () => { target = null })
    function move(direction) {
      target = stepTarget(viewport, rowTiles(root), target ?? viewport.scrollLeft, direction)
      viewport.scrollTo({
        left: target,
        behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
      })
    }
    root.querySelectorAll('[data-feed-media-step]').forEach(button => {
      button.addEventListener('click', event => {
        event.stopPropagation()
        move(Number(button.dataset.feedMediaStep))
      })
    })
    root.querySelector('[data-feed-media-fit-toggle]').addEventListener('click', event => {
      event.stopPropagation()
      root.toggleAttribute('data-feed-media-fitted')
      target = null
      syncFitToggle(root)
      layoutMediaRow(root)
    })
    root.addEventListener('keydown', event => {
      // Video controls retain their own arrow-key seek and volume behavior.
      if (event.target.closest('[data-feed-media-kind="video"]')) return
      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
      if (viewport.scrollWidth <= viewport.clientWidth + 1) return
      event.preventDefault()
      event.stopPropagation()
      move(event.key === 'ArrowRight' ? 1 : -1)
    })
    viewport.addEventListener('scroll', () => updateNavigation(root), { passive: true })
    syncFitToggle(root)
    layoutMediaRow(root)
  })
}

// One observer follows the feed width, including sidebar resizing; rows don't
// retain observers when navigation replaces or removes their DOM.
const content = document.getElementById('main-content')
if (content) {
  new ResizeObserver(() => {
    content.querySelectorAll('.feed-media-row-container').forEach(layoutMediaRow)
  }).observe(content)
}
