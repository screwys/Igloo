// Keep overflowing post media in one continuous, keyboard-accessible row.
export function mediaRowTarget(position, viewport, extent, direction) {
  const distance = Math.max(0, extent - viewport)
  const steps = Math.max(Math.ceil(distance / Math.max(1, viewport)), 1)
  return Math.max(0, Math.min(distance, position + direction * distance / steps))
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
}

export function initMediaRows(scope) {
  scope.querySelectorAll('.feed-media-row-container').forEach(root => {
    if (root.dataset.mediaRowReady) return
    root.dataset.mediaRowReady = '1'
    const viewport = root.querySelector('[data-feed-media-scroll]')
    let target = null
    for (const event of ['pointerdown', 'touchstart', 'wheel']) {
      viewport.addEventListener(event, () => { target = null }, { passive: true })
    }
    function move(direction) {
      target = mediaRowTarget(target ?? viewport.scrollLeft, viewport.clientWidth, viewport.scrollWidth, direction)
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
    updateNavigation(root)
  })
}

// One observer follows the feed width, including sidebar resizing; rows don't
// retain observers when navigation replaces or removes their DOM.
const content = document.getElementById('main-content')
if (content) {
  new ResizeObserver(() => {
    content.querySelectorAll('.feed-media-row-container').forEach(updateNavigation)
  }).observe(content)
}
