import { apiFetch, cssEscape } from '../utils.js'

const pending = new Map()

function ownerMedia(ownerID) {
  return document.querySelectorAll('[data-feed-media-owner-id="' + cssEscape(ownerID) + '"]')
}

function applyMediaState(ownerID, state) {
  ownerMedia(ownerID).forEach(element => {
    const index = Number(element.getAttribute('data-feed-media-index') || 0)
    const url = state.slide_urls && state.slide_urls[index]
    if (!url) return
    const image = element.querySelector('.feed-media-image')
    const video = element.querySelector('video')
    if (image && !image.naturalWidth && image.getAttribute('src') !== url) {
      element.setAttribute('data-feed-media-url', url)
      image.src = url
    }
    if (video && video.error) {
      element.setAttribute('data-feed-media-url', url)
      element.setAttribute('data-feed-media-stream', url)
      const source = video.querySelector('source')
      if (source) source.src = url
      else video.src = url
      video.load()
    }
  })
}

function refreshOwnerMedia(ownerID) {
  if (pending.has(ownerID)) return pending.get(ownerID)
  const request = (async function () {
    while (ownerMedia(ownerID).length) {
      const state = await apiFetch('/api/feed/media/' + encodeURIComponent(ownerID))
      applyMediaState(ownerID, state)
      if (!state.pending) return state
      await new Promise(resolve => window.setTimeout(resolve, 1000))
    }
    return {}
  })().catch(function () {
    return {}
  }).finally(function () {
    pending.delete(ownerID)
  })
  pending.set(ownerID, request)
  return request
}

export function initFeedMedia(scope) {
  scope.querySelectorAll('[data-feed-media-kind="image"] .feed-media-image').forEach(image => {
    if (image.dataset.feedMediaBound) return
    image.dataset.feedMediaBound = '1'
    const ownerID = image.closest('[data-feed-media-owner-id]').getAttribute('data-feed-media-owner-id')
    const refresh = () => refreshOwnerMedia(ownerID)
    image.addEventListener('error', refresh)
    if (image.complete && !image.naturalWidth) refresh()
  })
}

// An explicit open also restores media that was pruned or failed to download.
export function retryFeedMedia(trigger) {
  const original = trigger.getAttribute('data-feed-media-url') || trigger.getAttribute('data-feed-media-stream') || ''
  const ownerID = trigger.getAttribute('data-feed-media-owner-id')
  const image = trigger.querySelector('.feed-media-image')
  const video = trigger.querySelector('video')
  if (!ownerID || original.startsWith('/api/media/') &&
      (image && image.complete && image.naturalWidth > 0 || video && !video.error)) {
    return Promise.resolve(original)
  }
  const index = Number(trigger.getAttribute('data-feed-media-index') || 0)
  return apiFetch('/api/feed/media/retry', {
    method: 'POST', body: JSON.stringify({ tweet_id: ownerID }),
  }).then(async function () {
    const state = await refreshOwnerMedia(ownerID)
    if (video && !video.error) video.play().catch(function () {})
    return state.slide_urls && state.slide_urls[index] || ''
  }).catch(function () {
    return ''
  })
}
