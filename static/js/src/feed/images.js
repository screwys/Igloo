const imageSelector = '.feed-media-image[src], .feed-overlay-image[src]'
const imageStates = new WeakMap()
const failedImages = new Set()
let imageObserver = null

function eachImage(scope, visit) {
  if (scope.matches?.(imageSelector)) visit(scope)
  scope.querySelectorAll(imageSelector).forEach(visit)
}

function stopRetry(image, state) {
  clearTimeout(state.timer)
  state.timer = 0
  state.failed = false
  state.visible = false
  state.attempts = 0
  imageObserver?.unobserve(image)
  failedImages.delete(image)
}

function scheduleRetry(image, state) {
  if (state.timer || !state.failed || !state.visible || document.hidden) return
  const delay = Math.min(2000 * 2 ** Math.min(state.attempts - 1, 4), 30000)
  state.timer = setTimeout(() => {
    state.timer = 0
    if (!image.isConnected) {
      stopRetry(image, state)
      return
    }
    if (!state.visible || document.hidden) return
    state.failed = false
    const url = new URL(state.source, document.baseURI)
    url.searchParams.set('image_retry', String(Date.now()))
    image.src = url.href
  }, delay)
}

function ensureImageObserver() {
  if (imageObserver) return imageObserver
  imageObserver = new IntersectionObserver(entries => {
    entries.forEach(entry => {
      const image = entry.target
      const state = imageStates.get(image)
      if (!image.isConnected) {
        stopRetry(image, state)
        return
      }
      state.visible = entry.isIntersecting
      if (state.visible) scheduleRetry(image, state)
      else {
        clearTimeout(state.timer)
        state.timer = 0
      }
    })
  })
  return imageObserver
}

export function initFeedImages(scope) {
  eachImage(scope, image => {
    if (imageStates.has(image) || !image.getAttribute('src')) return
    const state = { source: image.getAttribute('src'), timer: 0, attempts: 0, failed: false, visible: false }
    imageStates.set(image, state)
    function failed() {
      if (!image.isConnected) return
      state.failed = true
      state.attempts++
      failedImages.add(image)
      ensureImageObserver().observe(image)
      scheduleRetry(image, state)
    }
    image.addEventListener('error', failed)
    image.addEventListener('load', () => stopRetry(image, state))
    if (image.complete && image.naturalWidth === 0) failed()
  })
}

export function disposeFeedImages(scope) {
  eachImage(scope, image => {
    const state = imageStates.get(image)
    if (state) stopRetry(image, state)
  })
}

document.addEventListener('visibilitychange', () => {
  failedImages.forEach(image => {
    const state = imageStates.get(image)
    clearTimeout(state.timer)
    state.timer = 0
    if (!image.isConnected) stopRetry(image, state)
    else if (!document.hidden) scheduleRetry(image, state)
  })
})
