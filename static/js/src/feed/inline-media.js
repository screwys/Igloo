// Inline media module — extracted from feed_page.js
// IntersectionObserver-based autoplay/pause for inline feed videos.

import { bindFeedVideoControls } from './video-controls.js'

let observer = null
let preloadObserver = null

function preloadVideo(video) {
  if (!(video instanceof HTMLVideoElement)) return
  if (video.dataset.feedPreloadStarted === '1') return
  video.dataset.feedPreloadStarted = '1'
  if (video.preload === 'none') video.preload = 'metadata'
  try { video.load() } catch (_) { }
}

function ensurePreloadObserver() {
  if (preloadObserver) return preloadObserver
  preloadObserver = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      const video = entry.target
      if (!(video instanceof HTMLVideoElement)) return
      if (!entry.isIntersecting) return
      preloadVideo(video)
      preloadObserver.unobserve(video)
    })
  }, { rootMargin: '900px 0px 900px 0px', threshold: [0] })
  return preloadObserver
}

function ensureObserver() {
  if (observer) return observer
  observer = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      const video = entry.target
      if (!(video instanceof HTMLVideoElement)) return
      if (video.hasAttribute('data-feed-overlay-active')) return
      if (entry.isIntersecting && entry.intersectionRatio >= 0.55) {
        video.muted = true
        preloadVideo(video)
        video.play().catch(function () { })
      } else {
        try { video.pause() } catch (_) { }
      }
    })
  }, { threshold: [0.35, 0.55, 0.8] })
  return observer
}

function bindVideo(video) {
  if (!(video instanceof HTMLVideoElement)) return false
  if (video.dataset.feedBound === '1') return false
  video.dataset.feedBound = '1'
  const wrap = video.closest('.feed-media-wrap')
  bindFeedVideoControls(wrap, video)

  ensurePreloadObserver().observe(video)
  ensureObserver().observe(video)
  return true
}

export function initInlineMedia(container) {
  const scope = container || document
  const videos = Array.from(scope.querySelectorAll('video[data-feed-inline-video]'))
  const newlyBound = videos.filter(bindVideo)
  newlyBound.slice(0, 3).forEach(preloadVideo)
}

// Global bridge for initFeedCards and other callers
window.FeedInlineMedia = { init: initInlineMedia }
