import { apiFetch } from '../utils.js'

const pending = new WeakMap()

// An explicit open restores uncached media without making feed scrolling fetch it.
export function retryFeedMedia(trigger) {
  const original = trigger.getAttribute('data-feed-media-url') || trigger.getAttribute('data-feed-media-stream') || ''
  const ownerID = trigger.getAttribute('data-feed-media-owner-id')
  const image = trigger.querySelector('.feed-media-image')
  const video = trigger.querySelector('video')
  if (!ownerID || original.startsWith('/api/media/') &&
      (image && image.complete && image.naturalWidth > 0 || video && !video.error)) {
    return Promise.resolve(original)
  }
  if (pending.has(trigger)) return pending.get(trigger)
  const index = Number(trigger.getAttribute('data-feed-media-index') || 0)
  const request = apiFetch('/api/feed/media/retry', {
    method: 'POST', body: JSON.stringify({ tweet_id: ownerID }),
  }).then(async function () {
    while (trigger.isConnected) {
      const state = await apiFetch('/api/feed/media/' + encodeURIComponent(ownerID))
      const url = state.slide_urls && state.slide_urls[index]
      if (url) {
        trigger.setAttribute('data-feed-media-url', url)
        if (image) image.src = url
        if (video && video.error) {
          trigger.setAttribute('data-feed-media-stream', url)
          const source = video.querySelector('source')
          if (source) source.src = url
          else video.src = url
          video.load()
          video.play().catch(function () {})
        }
        return url
      }
      if (!state.pending) return ''
      await new Promise(resolve => window.setTimeout(resolve, 1000))
    }
    return ''
  }).catch(function () {
    return ''
  }).finally(function () {
    pending.delete(trigger)
  })
  pending.set(trigger, request)
  return request
}
