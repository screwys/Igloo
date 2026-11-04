// Media viewer with an optional post panel.

import {
  itemRootFromNode,
  stateBool,
  getFeedActionIconSvg,
  syncFeedActionIcons,
  materialIconMarkup,
  apiFetch,
  t,
} from '../utils.js'
import { bindFeedVideoControls, createFeedVideoControls, handleFeedVideoShortcut } from './video-controls.js'
import { createLiveChatPanel, initBroadcastChat } from '../player/chat.js'
import { createXLivePlayback } from '../player/x-live.js'
import { initFeedImages, disposeFeedImages } from './images.js'
import { formatFullDate } from './dates.js'
import { initMediaRows } from './media-row.js'

// ── Helpers ──

function textContentTrim(node) {
  return String((node && node.textContent) || '').trim()
}

function safeExternalHttpURL(raw) {
  const value = String(raw || '').trim()
  if (!/^https?:\/\//i.test(value)) return ''
  try {
    const parsed = new URL(value)
    return (parsed.protocol === 'http:' || parsed.protocol === 'https:') ? parsed.href : ''
  } catch (_) {
    return ''
  }
}

function getRetweetMutedChannels() {
  let raw = ''
  try {
    raw = localStorage.getItem('feedMutedRetweetChannels') || ''
    if (!raw) raw = localStorage.getItem('mpa-feed-retweet-muted:v1') || ''
  } catch (_) { raw = '' }
  if (!raw) return new Set()
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.map(function (v) { return String(v || '').trim() }).filter(Boolean))
  } catch (_) { return new Set() }
}

function updateRetweetMenuLabels(scope) {
  const root = scope || document
  root.querySelectorAll('[data-feed-menu-action="retweets_off"]').forEach(function (btn) {
    const channelId = String(btn.getAttribute('data-feed-channel-id') || '').trim()
    const muted = channelId ? getRetweetMutedChannels().has(channelId) : false
    btn.textContent = muted
      ? t('feed_turn_on_retweets', 'Turn on retweets')
      : t('feed_turn_off_retweets', 'Turn off retweets')
  })
}

// ── Module state ──

let overlayEl = null
let keyHandler = null

function focusOverlay(overlay, trigger) {
  overlay._returnFocus = trigger || overlay.ownerDocument.activeElement
  overlay.setAttribute('role', 'dialog')
  overlay.setAttribute('aria-modal', 'true')
  overlay.setAttribute('aria-label', t('feed_post_media', 'Post media'))
  overlay.querySelector('.feed-media-overlay-close').focus({ preventScroll: true })
}

function stopLivePlayback(overlay) {
  if (overlay && overlay._livePlayback) return overlay._livePlayback.stop()
}

function setFeedMediaOverlayOpen(open, ownerDocument = document) {
  if (!ownerDocument.body) return
  ownerDocument.body.classList.toggle('feed-media-overlay-open', !!open)
}

function takeOverInlineVideoPlayback(overlay, activeStreamUrl) {
  var sourceVideo = null
  document.querySelectorAll('video[data-feed-inline-video]').forEach(function (video) {
    var wrap = video.closest && video.closest('[data-feed-media]')
    if (!sourceVideo && wrap && wrap.getAttribute('data-feed-media-stream') === activeStreamUrl) {
      sourceVideo = video
      return
    }
    try { video.pause() } catch (_) { }
  })

  if (!sourceVideo || !sourceVideo.parentNode) return null
  var placeholder = document.createElement('div')
  var bounds = sourceVideo.getBoundingClientRect()
  placeholder.style.width = bounds.width + 'px'
  placeholder.style.height = bounds.height + 'px'
  sourceVideo.parentNode.replaceChild(placeholder, sourceVideo)

  overlay._sourceVideo = sourceVideo
  overlay._sourcePlaceholder = placeholder
  overlay._sourceClassName = sourceVideo.className
  overlay._sourceMuted = sourceVideo.muted
  sourceVideo.setAttribute('data-feed-overlay-active', '1')
  sourceVideo.classList.add('feed-overlay-video')
  sourceVideo.muted = false
  return sourceVideo
}

function restoreInlineVideoPlayback(overlay, keepPlaying) {
  var sourceVideo = overlay && overlay._sourceVideo
  var placeholder = overlay && overlay._sourcePlaceholder
  if (!sourceVideo || !placeholder || !placeholder.parentNode) return
  if (!keepPlaying) {
    try { sourceVideo.pause() } catch (_) { }
  }
  if (overlay._videoClickHandler) sourceVideo.removeEventListener('click', overlay._videoClickHandler)
  if (overlay._videoControlsCleanup) overlay._videoControlsCleanup()
  sourceVideo.className = overlay._sourceClassName
  sourceVideo.muted = overlay._sourceMuted
  sourceVideo.removeAttribute('data-feed-overlay-active')
  placeholder.parentNode.replaceChild(sourceVideo, placeholder)
  overlay._sourceVideo = null
  overlay._sourcePlaceholder = null
  overlay._sourceClassName = ''
  overlay._sourceMuted = false
  overlay._overlayVideo = null
  overlay._overlayPlaybackKind = ''
  overlay._videoClickHandler = null
  overlay._videoControlsCleanup = null
}

// ── Media source extraction ──

// Extract slide list from any feed item subtree (parent article or quote card).
// Returns { slides, singleVideo } where:
//   slides    = array of { kind, url, streamUrl, posterUrl } (possibly empty)
//   singleVideo = { streamUrl, posterUrl } when root contains exactly one
//                 standalone video wrap (no grid), else null. Used to preserve
//                 the large-video fast path when the overall overlay has only
//                 one slide.
function extractSlidesFromRoot(rootEl) {
  if (!rootEl) return { slides: [], singleVideo: null }

  var rootIsQuote = rootEl.classList && rootEl.classList.contains('feed-quote-card')

  // Prefer grid if present at this scope.
  var grid = null
  var gridCandidates = rootEl.querySelectorAll('.feed-media-wrap-grid')
  for (var gi = 0; gi < gridCandidates.length; gi++) {
    var gc = gridCandidates[gi]
    if (rootIsQuote) { grid = gc; break }
    if (!gc.closest('.feed-quote-card')) { grid = gc; break }
  }

  if (grid) {
    var tiles = grid.querySelectorAll('.feed-media-tile')
    var slides = []
    tiles.forEach(function (tile) {
      var tKind = String(tile.getAttribute('data-feed-media-kind') || 'image').trim().toLowerCase()
      var tUrl = String(tile.getAttribute('data-feed-media-url') || '').trim()
      var tStream = String(tile.getAttribute('data-feed-media-stream') || '').trim()
      var tPoster = String(tile.getAttribute('data-feed-media-preview') || '').trim()
      if (!tUrl) {
        var img = tile.querySelector('.feed-media-image')
        if (img) tUrl = String(img.getAttribute('src') || '').trim()
      }
      var tPlaybackKind = String(tile.getAttribute('data-feed-video-kind') || 'video').trim().toLowerCase()
      slides.push({ kind: tKind, playbackKind: tPlaybackKind, url: tUrl, streamUrl: tStream, posterUrl: tPoster })
    })
    return { slides: slides, singleVideo: null }
  }

  // No grid. Look for a single .feed-media-wrap at this scope.
  var wraps = rootEl.querySelectorAll('.feed-media-wrap')
  var wrap = null
  for (var wi = 0; wi < wraps.length; wi++) {
    var w = wraps[wi]
    if (rootIsQuote) { wrap = w; break }
    if (!w.closest('.feed-quote-card')) { wrap = w; break }
  }
  if (!wrap) return { slides: [], singleVideo: null }

  var wKind = String(wrap.getAttribute('data-feed-media-kind') || '').trim().toLowerCase()
  if (wKind === 'video') {
    var streamUrl = String(wrap.getAttribute('data-feed-media-stream') || '').trim()
    var posterUrl = String(wrap.getAttribute('data-feed-media-preview') || '').trim()
    var playbackKind = String(wrap.getAttribute('data-feed-video-kind') || 'video').trim().toLowerCase()
    if (!posterUrl) {
      var vidEl = wrap.querySelector('video')
      if (vidEl) posterUrl = String(vidEl.getAttribute('poster') || '').trim()
    }
    return {
      slides: [{ kind: 'video', playbackKind: playbackKind, url: '', streamUrl: streamUrl, posterUrl: posterUrl }],
      singleVideo: { streamUrl: streamUrl, posterUrl: posterUrl, playbackKind: playbackKind },
    }
  }

  var imgUrl = String(wrap.getAttribute('data-feed-media-url') || '').trim()
  if (!imgUrl) {
    var imgEl = wrap.querySelector('.feed-media-image')
    if (imgEl) imgUrl = String(imgEl.getAttribute('src') || '').trim()
  }
  if (!imgUrl) return { slides: [], singleVideo: null }
  return {
    slides: [{ kind: 'image', url: imgUrl, streamUrl: '', posterUrl: '' }],
    singleVideo: null,
  }
}

function extractProfileMediaSlides(rootEl) {
  if (!rootEl || !rootEl.hasAttribute || !rootEl.hasAttribute('data-x-profile-media')) return null
  var nodes = rootEl.querySelectorAll('[data-x-profile-media-slide]')
  var slides = []
  nodes.forEach(function (node) {
    var kind = String(node.getAttribute('data-feed-media-kind') || 'image').trim().toLowerCase()
    var url = String(node.getAttribute('data-feed-media-url') || '').trim()
    var streamUrl = String(node.getAttribute('data-feed-media-stream') || '').trim()
    var posterUrl = String(node.getAttribute('data-feed-media-preview') || '').trim()
    var playbackKind = String(node.getAttribute('data-feed-video-kind') || 'video').trim().toLowerCase()
    if (!url && !streamUrl) return
    slides.push({ kind: kind, playbackKind: playbackKind, url: url, streamUrl: streamUrl, posterUrl: posterUrl })
  })
  if (!slides.length) return null
  var only = slides[0]
  return {
    slides: slides,
    singleVideo: slides.length === 1 && only.kind === 'video' && only.streamUrl
      ? { streamUrl: only.streamUrl, posterUrl: only.posterUrl, playbackKind: only.playbackKind }
      : null,
  }
}

function getMediaSources(card, clickedEl) {
  const root = itemRootFromNode(card) || card
  if (!root) return null
  const profileExtract = extractProfileMediaSlides(root)
  if (profileExtract) {
    const profileSlides = profileExtract.slides.map(function (slide) { return Object.assign({}, slide, { source: 'parent' }) })
    const only = profileSlides[0]
    if (profileSlides.length === 1 && only.kind === 'video' && profileExtract.singleVideo) {
      return {
        kind: 'video',
        streamUrl: profileExtract.singleVideo.streamUrl,
        posterUrl: profileExtract.singleVideo.posterUrl,
        playbackKind: profileExtract.singleVideo.playbackKind,
        urls: [],
        slides: profileSlides,
        startIndex: 0,
      }
    }
    return {
      kind: profileSlides.some(function (slide) { return slide.kind === 'video' }) || profileSlides.length > 1 ? 'mixed' : 'image',
      slides: profileSlides,
      urls: profileSlides.map(function (slide) { return slide.url }),
      streamUrl: '',
      posterUrl: '',
      startIndex: 0,
    }
  }
  const trigger = clickedEl && clickedEl.closest ? clickedEl.closest('[data-feed-media]') : null
  const triggerInQuote = !!(trigger && trigger.closest && trigger.closest('.feed-quote-card'))
  const quoteCardEl = root.querySelector('.feed-quote-card')
  const parentExtract = extractSlidesFromRoot(root)
  const quoteExtract = quoteCardEl ? extractSlidesFromRoot(quoteCardEl) : { slides: [], singleVideo: null }
  const parentSlides = parentExtract.slides.map(function (s) { return Object.assign({}, s, { source: 'parent' }) })
  const quoteSlides = quoteExtract.slides.map(function (s) { return Object.assign({}, s, { source: 'quote' }) })
  const slides = parentSlides.concat(quoteSlides)

  if (slides.length === 0) return null

  // Preserve the single-standalone-video fast path only when the entire overlay
  // is that one video (no grid, no other side). Mixed / multi-slide cases always
  // use the slides[] path.
  if (slides.length === 1) {
    var only = slides[0]
    var onlyFromParent = parentSlides.length === 1
    var onlySingleVideo = onlyFromParent ? parentExtract.singleVideo : quoteExtract.singleVideo
    if (only.kind === 'video' && onlySingleVideo) {
      return {
        kind: 'video',
        streamUrl: onlySingleVideo.streamUrl,
        posterUrl: onlySingleVideo.posterUrl,
        playbackKind: onlySingleVideo.playbackKind,
        urls: [],
        slides: slides,
        startIndex: 0,
      }
    }
  }

  // Pick start index based on click origin.
  var startIndex = 0
  if (trigger) {
    var triggerUrl = String(trigger.getAttribute('data-feed-media-url') || '').trim()
    var triggerStream = String(trigger.getAttribute('data-feed-media-stream') || '').trim()
    var rangeFrom = triggerInQuote ? parentSlides.length : 0
    var rangeTo = triggerInQuote ? slides.length : parentSlides.length
    for (var si = rangeFrom; si < rangeTo; si++) {
      var s = slides[si]
      if (triggerUrl && s.url === triggerUrl) { startIndex = si; break }
      if (triggerStream && s.streamUrl === triggerStream) { startIndex = si; break }
    }
    if (rangeFrom >= rangeTo) startIndex = 0
  }

  const anyVideo = slides.some(function (s) { return s.kind === 'video' })
  const urls = slides.map(function (s) { return s.url })

  return {
    kind: anyVideo || slides.length > 1 ? 'mixed' : 'image',
    slides: slides,
    urls: urls,
    streamUrl: '',
    posterUrl: '',
    playbackKind: '',
    startIndex: startIndex,
  }
}

// ── Close overlay ──

export function closeMediaOverlay() {
  const ownerDocument = overlayEl ? overlayEl.ownerDocument : document
  const returnFocus = overlayEl && overlayEl._returnFocus
  if (overlayEl) {
    if (overlayEl._layoutCleanup) overlayEl._layoutCleanup()
    if (overlayEl._contextAbort) overlayEl._contextAbort.abort()
    if (overlayEl._spaceSurface) overlayEl._spaceSurface.restore()
    if (overlayEl.hasAttribute('data-feed-live')) {
      window.removeEventListener('pagehide', closeMediaOverlay)
      window.removeEventListener('popstate', closeMediaOverlay)
      if (overlayEl._liveChatCleanup) overlayEl._liveChatCleanup()
      if (overlayEl._videoControlsCleanup) overlayEl._videoControlsCleanup()
    }
    stopLivePlayback(overlayEl)
    restoreInlineVideoPlayback(overlayEl, true)
    disposeFeedImages(overlayEl)
    if (overlayEl.parentNode) overlayEl.remove()
  }
  overlayEl = null
  setFeedMediaOverlayOpen(false, ownerDocument)
  if (keyHandler) {
    ownerDocument.removeEventListener('keydown', keyHandler)
    keyHandler = null
  }
  if (returnFocus && returnFocus.isConnected) returnFocus.focus({ preventScroll: true })
}

// ── Open overlay ──

function openSpaceOverlay(surface) {
  closeMediaOverlay()
  const ownerDocument = surface.row.ownerDocument
  const overlay = ownerDocument.createElement('div')
  overlay.className = 'feed-media-overlay feed-space-overlay'
  const shell = ownerDocument.createElement('div')
  shell.className = 'feed-media-overlay-shell'
  const main = ownerDocument.createElement('div')
  main.className = 'feed-media-overlay-main'
  const close = ownerDocument.createElement('button')
  close.className = 'feed-media-overlay-close'
  close.type = 'button'
  close.title = t('space_collapse', 'Collapse')
  close.setAttribute('aria-label', close.title)
  close.innerHTML = materialIconMarkup('KeyboardArrowDown') // eslint-disable-line no-unsanitized/property
  shell.appendChild(close)
  main.appendChild(surface.element)
  shell.appendChild(main)
  overlay.appendChild(shell)
  overlay._spaceSurface = surface
  overlay._overlayVideo = surface.video
  overlay._overlayPlaybackKind = 'live'
  overlayEl = overlay
  ownerDocument.body.appendChild(overlay)
  setFeedMediaOverlayOpen(true, ownerDocument)
  close.addEventListener('click', closeMediaOverlay)
  overlay.addEventListener('click', function (event) {
    if (event.target === overlay) closeMediaOverlay()
  })
  keyHandler = function (event) {
    const target = event.target || {}
    if (/^(input|textarea|select)$/i.test(target.tagName || '') || target.isContentEditable) return
    if (event.key === 'Escape') {
      if (!ownerDocument.fullscreenElement && !ownerDocument.webkitFullscreenElement) closeMediaOverlay()
    } else if (handleFeedVideoShortcut(event, surface.video)) {
      event.preventDefault()
      event.stopImmediatePropagation()
    }
  }
  ownerDocument.addEventListener('keydown', keyHandler)
  focusOverlay(overlay, surface.row)
}

export function openMediaOverlay(root, triggerEl, spaceSurface) {
  if (spaceSurface) {
    openSpaceOverlay(spaceSurface)
    return
  }
  const liveURL = root && root.getAttribute('data-feed-live-url') || ''
  const media = liveURL ? {
    kind: 'video', streamUrl: liveURL, posterUrl: root.getAttribute('data-feed-live-thumbnail') || '', playbackKind: 'live',
  } : getMediaSources(root, triggerEl)
  if (!media) return
  closeMediaOverlay()
  const article = itemRootFromNode(root) || root
  const tweetId = String(article.getAttribute('data-tweet-id') || '').trim()
  const link = safeExternalHttpURL(article.getAttribute('data-feed-link'))
  let channelId = String(article.getAttribute('data-channel-id') || '').trim()
  let channelName = String(article.getAttribute('data-channel-name') || '').trim()

  const quoteCardEl = article.querySelector('.feed-quote-card')
  const quoteTweetId = quoteCardEl ? String(quoteCardEl.getAttribute('data-quote-tweet-id') || '').trim() : ''
  const quoteLink = quoteCardEl ? safeExternalHttpURL(quoteCardEl.getAttribute('data-quote-link')) : ''

  let currentIndex = Math.max(0, Number(media.startIndex || 0))
  const overlay = document.createElement('div')
  overlay.className = 'feed-media-overlay'
  overlay._feedRoot = article
  if (!liveURL) overlay.classList.add('feed-post-overlay')
  if (liveURL) overlay.setAttribute('data-feed-live', '')
  if (tweetId) overlay.setAttribute('data-feed-overlay-tweet-id', tweetId)
  // Quote overlay attribute is toggled per slide by renderSidebar.
  // Static overlay shell template — no user input
  overlay.innerHTML = '' + // eslint-disable-line no-unsanitized/property
    '<div class="feed-media-overlay-shell">' +
    '<div class="feed-media-overlay-main">' +
    '<div class="feed-media-overlay-left">' +
    '<button class="feed-media-overlay-close" type="button" title="' + t('action_close', 'Close') + '" aria-label="' + t('action_close', 'Close') + '">' + materialIconMarkup('Close') + '</button>' +
    '<button class="feed-media-overlay-panel-toggle" type="button" aria-controls="feed-media-post-panel"></button>' +
    '<div class="feed-media-overlay-menu feed-card-menu" data-feed-menu>' +
    '<button class="feed-card-menu-btn" type="button" data-feed-menu-toggle title="' + t('feed_post_actions', 'Post actions') + '" aria-label="' + t('feed_post_actions', 'Post actions') + '">' + materialIconMarkup('MoreHoriz') + '</button>' +
    '<div class="feed-card-menu-list"><a class="feed-card-menu-item" data-feed-overlay-view-post>' + t('feed_view_post', 'View post') + '</a></div>' +
    '</div>' +
    '<button class="feed-media-overlay-nav prev" type="button" aria-label="' + t('action_previous', 'Previous') + '">' + materialIconMarkup('KeyboardArrowLeft') + '</button>' +
    '<div class="feed-media-overlay-media"></div>' +
    '<button class="feed-media-overlay-nav next" type="button" aria-label="' + t('action_next', 'Next') + '">' + materialIconMarkup('KeyboardArrowRight') + '</button>' +
    '<div class="feed-media-overlay-bottom"></div>' +
    '</div>' +
    '<div class="feed-media-overlay-right" id="feed-media-post-panel">' +
    '<div class="feed-media-overlay-top"></div>' +
    '<div class="feed-media-overlay-thread-controls thread-fetch-controls" hidden></div>' +
    '<div class="feed-media-overlay-context"></div>' +
    '</div>' +
    '</div>' +
    '</div>'
  document.body.appendChild(overlay)
  overlayEl = overlay
  if (liveURL) {
    window.addEventListener('pagehide', closeMediaOverlay)
    window.addEventListener('popstate', closeMediaOverlay)
  }
  setFeedMediaOverlayOpen(true)

  const top = overlay.querySelector('.feed-media-overlay-top')
  const host = overlay.querySelector('.feed-media-overlay-media')
  const prev = overlay.querySelector('.feed-media-overlay-nav.prev')
  const next = overlay.querySelector('.feed-media-overlay-nav.next')
  const bottom = overlay.querySelector('.feed-media-overlay-bottom')
  const right = overlay.querySelector('.feed-media-overlay-right')
  const panelToggle = overlay.querySelector('.feed-media-overlay-panel-toggle')
  const viewPost = overlay.querySelector('[data-feed-overlay-view-post]')
  const context = overlay.querySelector('.feed-media-overlay-context')
  const threadControls = overlay.querySelector('.feed-media-overlay-thread-controls')
  const fetchThreadButton = document.createElement('button')
  fetchThreadButton.type = 'button'
  fetchThreadButton.className = 'feed-text-toggle'
  fetchThreadButton.textContent = t('feed_thread_fetch', 'Fetch replies and quote-posts')
  fetchThreadButton.addEventListener('click', function () { refreshThreadContext(currentTweetId()) })
  const threadStatus = document.createElement('div')
  threadStatus.className = 'thread-fetch-status'
  threadStatus.setAttribute('role', 'status')
  threadStatus.setAttribute('aria-live', 'polite')
  threadControls.append(fetchThreadButton, threadStatus)
  const wideLayout = window.matchMedia('(min-width: 1100px)')
  const smallLayout = window.matchMedia('(max-width: 639px)')
  let panelExpanded = wideLayout.matches
  let activeSource = 'parent'
  let renderedSource = null
  const threadContexts = new Map()
  overlay._contextAbort = new AbortController()

  function updatePanel() {
    const visible = liveURL || (panelExpanded && !smallLayout.matches)
    overlay.classList.toggle('feed-media-panel-open', !!visible)
    right.hidden = !visible
    panelToggle.setAttribute('aria-expanded', String(!!visible))
    panelToggle.title = visible ? t('feed_hide_post', 'Hide post') : t('feed_view_post', 'View post')
    panelToggle.setAttribute('aria-label', panelToggle.title)
    panelToggle.innerHTML = materialIconMarkup(visible ? 'KeyboardDoubleArrowRight' : 'KeyboardDoubleArrowLeft') // eslint-disable-line no-unsanitized/property
    if (visible && !liveURL) {
      initMediaRows(context)
      ensureThreadContext()
    }
  }
  function layoutChanged() {
    const panelFocused = right.contains(document.activeElement) || document.activeElement === panelToggle
    panelExpanded = wideLayout.matches
    updatePanel()
    if (panelFocused && right.hidden) {
      const control = smallLayout.matches ? overlay.querySelector('.feed-media-overlay-close') : panelToggle
      control.focus({ preventScroll: true })
    }
  }
  wideLayout.addEventListener('change', layoutChanged)
  smallLayout.addEventListener('change', layoutChanged)
  overlay._layoutCleanup = function () {
    wideLayout.removeEventListener('change', layoutChanged)
    smallLayout.removeEventListener('change', layoutChanged)
  }
  panelToggle.addEventListener('click', function () {
    panelExpanded = !panelExpanded
    updatePanel()
  })
  let liveChatPanel = liveURL ? createLiveChatPanel() : null
  if (liveChatPanel) {
    right.classList.add('player-sidebar')
    right.replaceChildren(liveChatPanel)
  }

  function removePostMedia(post) {
    post.querySelectorAll('[data-media-owner]').forEach(function (node) {
      if (!node.querySelector('.feed-quote-unavailable')) node.remove()
    })
    post.querySelectorAll('[data-feed-media]').forEach(function (node) { node.remove() })
  }

  function copyPost(source, includeMedia = false) {
    const post = source.cloneNode(true)
    post.classList.add('feed-overlay-post')
    post.classList.remove('feed-card-seen', 'feed-card-muted-retweet', 'feed-thread-leaf')
    post.style.display = ''
    if (!includeMedia) removePostMedia(post)
    post.querySelectorAll('.feed-text-toggle, [data-feed-video-controls]').forEach(function (node) { node.remove() })
    post.querySelectorAll('[data-feed-text-clamp]').forEach(function (node) {
      node.classList.remove('feed-text-clamp', 'expanded')
      node.style.removeProperty('display')
      node.style.removeProperty('-webkit-box-orient')
      node.style.removeProperty('-webkit-line-clamp')
      node.style.removeProperty('line-clamp')
      node.removeAttribute('data-feed-text-clamp')
    })
    post.querySelectorAll('[id]').forEach(function (node) { node.removeAttribute('id') })
    post.querySelectorAll('.feed-card-header, .feed-quote-head').forEach(function (header) {
      const author = header.querySelector('.feed-author-wrap, .feed-quote-head-main')
      const name = author && author.querySelector('.feed-author-trigger, .feed-quote-author')
      if (!name) return
      const identity = document.createElement('div')
      identity.className = 'feed-overlay-identity'
      identity.appendChild(name)
      const badge = author.querySelector('.account-details-wrap')
      if (badge) identity.appendChild(badge)
      author.prepend(identity)

      const owner = header.parentNode
      const date = header.querySelector('[data-feed-date-raw]')
      if (date) {
        date.setAttribute('data-date-format', 'full')
        date.removeAttribute('data-date-relative')
        date.textContent = formatFullDate(date.getAttribute('data-feed-date-raw'))
        const dateRow = document.createElement('div')
        dateRow.className = 'feed-overlay-date'
        dateRow.appendChild(date)
        owner.insertBefore(dateRow, owner.querySelector(':scope > .feed-actions'))
      }
      const translate = header.querySelector('.feed-translate-btn')
      const handle = author.querySelector('.feed-author-handle')
      if (handle || translate) {
        const details = document.createElement('div')
        details.className = 'feed-overlay-author-details'
        if (handle) details.appendChild(handle)
        if (translate) details.appendChild(translate)
        author.appendChild(details)
      }
      header.querySelectorAll('.feed-date-inline, .feed-quote-date').forEach(function (node) { node.remove() })
    })
    return post
  }

  function profilePost() {
    const post = document.createElement('article')
    post.className = 'feed-overlay-post channel-row'
    const headline = document.createElement('a')
    headline.className = 'feed-overlay-headline'
    headline.href = channelId ? '/channels/' + encodeURIComponent(channelId) : link
    const avatar = article.querySelector('.feed-avatar')
    if (avatar) headline.appendChild(avatar.cloneNode(true))
    const author = document.createElement('span')
    author.className = 'feed-overlay-author'
    author.textContent = textContentTrim(article.querySelector('.feed-author')) || channelName
    headline.appendChild(author)
    post.appendChild(headline)
    const body = article.querySelector('.feed-body-text')
    if (body) {
      const text = body.cloneNode(true)
      text.className = 'feed-overlay-text'
      post.appendChild(text)
    }
    const date = document.createElement('div')
    const rawDate = article.getAttribute('data-feed-date') || ''
    date.className = 'feed-overlay-date feed-date-inline'
    date.setAttribute('data-feed-date-raw', rawDate)
    date.setAttribute('data-date-format', 'full')
    date.setAttribute('data-timestamp', rawDate)
    date.textContent = formatFullDate(rawDate)
    post.appendChild(date)
    return post
  }

  function currentTweetId() {
    return activeSource === 'quote' ? quoteTweetId : tweetId
  }

  function contextFor(id) {
    if (!threadContexts.has(id)) {
      threadContexts.set(id, { cards: null, request: null, fetchState: '' })
    }
    return threadContexts.get(id)
  }

  function renderThreadControls() {
    const state = contextFor(currentTweetId()).fetchState
    const autoFetch = (window.IglooPreferences || {}).threadFetchOnOpen !== false
    fetchThreadButton.hidden = state === 'loading' || state === 'done' || (autoFetch && state !== 'error')
    threadStatus.hidden = state !== 'loading' && state !== 'error'
    threadControls.hidden = !currentTweetId() || (fetchThreadButton.hidden && threadStatus.hidden)
    threadStatus.setAttribute('data-thread-fetch-state', state)
    threadStatus.textContent = state === 'loading'
      ? t('feed_thread_fetching', 'Fetching replies and quote-posts…')
      : state === 'error' ? t('error_refresh_failed', 'Refresh failed') : ''
  }

  function renderContext() {
    const id = currentTweetId()
    const cards = contextFor(id).cards
    renderThreadControls()
    disposeFeedImages(context)
    context.replaceChildren()
    if (!Array.isArray(cards)) return
    const related = cards.filter(function (card) { return card.getAttribute('data-tweet-id') !== id })
    if (!related.length) return
    const heading = document.createElement('h2')
    heading.textContent = t('feed_replies', 'Replies')
    context.appendChild(heading)
    related.forEach(function (card) { context.appendChild(card) })
    updateRetweetMenuLabels(context)
    syncFeedActionIcons(context)
    initFeedImages(context)
    if (!right.hidden) initMediaRows(context)
    if (window.htmx) window.htmx.process(context)
  }

  function loadThreadContext(id, refresh = false) {
    if (!id) return Promise.resolve()
    const saved = contextFor(id)
    if (saved.request) return saved.request
    if (saved.cards && !refresh) return Promise.resolve()
    saved.request = fetch('/thread/' + encodeURIComponent(id) + '?fmt=partial', { signal: overlay._contextAbort.signal })
      .then(function (response) {
        if (!response.ok) throw new Error('Thread unavailable')
        return response.text()
      })
      .then(function (html) {
        if (overlayEl !== overlay) return
        const page = new DOMParser().parseFromString(html, 'text/html')
        saved.cards = Array.from(page.querySelectorAll('[data-thread-page] .feed-card[data-feed-item]'), function (card) {
          return copyPost(card, true)
        })
        if (currentTweetId() !== id) return
        if (article.hasAttribute('data-x-profile-media')) renderSidebar(activeSource)
        else renderContext()
      })
      .catch(function (error) {
        if (refresh) throw error
        // Keep the current post readable if saved context cannot load.
        if (!saved.cards) saved.cards = []
      })
      .finally(function () { saved.request = null })
    return saved.request
  }

  function ensureThreadContext() {
    const id = currentTweetId()
    if (!id) return
    const saved = contextFor(id)
    if ((window.IglooPreferences || {}).threadFetchOnOpen !== false && !saved.fetchState) {
      refreshThreadContext(id)
    } else {
      loadThreadContext(id)
    }
  }

  async function refreshThreadContext(id) {
    const saved = contextFor(id)
    if (saved.fetchState === 'loading') return
    saved.fetchState = 'loading'
    renderThreadControls()
    try {
      await loadThreadContext(id)
      if (overlayEl !== overlay) return
      let captureError = null
      try {
        await apiFetch('/api/thread/' + encodeURIComponent(id) + '/refresh', {
          method: 'POST', signal: overlay._contextAbort.signal,
        })
      } catch (error) {
        if (!error.payload || !error.payload.fetched) throw error
        captureError = error
      }
      if (overlayEl !== overlay) return
      await loadThreadContext(id, true)
      saved.fetchState = captureError ? 'error' : 'done'
    } catch (_) {
      saved.fetchState = 'error'
    } finally {
      if (overlayEl === overlay && currentTweetId() === id) renderThreadControls()
    }
  }

  function renderSidebar(sourceKind) {
    if (liveURL) return
    const isQuote = sourceKind === 'quote' && !!quoteCardEl
    const sourceCard = isQuote ? quoteCardEl : article
    activeSource = sourceKind
    const activeTweetId = isQuote ? quoteTweetId : tweetId
    viewPost.href = '/thread/' + encodeURIComponent(activeTweetId)
    viewPost.hidden = !activeTweetId

    // ── Ownership attribute — downstream handlers in feed/index.js read this.
    if (isQuote && quoteTweetId) {
      overlay.setAttribute('data-feed-overlay-quote-tweet-id', quoteTweetId)
    } else {
      overlay.removeAttribute('data-feed-overlay-quote-tweet-id')
    }

    if (top) {
      let post
      if (isQuote) {
        post = document.createElement('article')
        post.className = 'feed-overlay-post channel-row'
        post.setAttribute('data-feed-item', '')
        post.setAttribute('data-tweet-id', tweetId)
        post.appendChild(copyPost(sourceCard))
      } else if (article.classList.contains('feed-card')) {
        post = copyPost(article)
      } else {
        const stored = contextFor(activeTweetId).cards
        const fullPost = Array.isArray(stored) && stored.find(function (card) {
          return card.getAttribute('data-tweet-id') === activeTweetId
        })
        post = fullPost || profilePost()
        if (fullPost) removePostMedia(post)
      }
      top.replaceChildren(post)
      updateRetweetMenuLabels(top)
      syncFeedActionIcons(top)
      if (window.htmx) window.htmx.process(top)
      renderContext()
      ensureThreadContext()
    }

    // ── Bottom (share / heart / bookmark / open-on-X) ──
    if (bottom) {
      while (bottom.firstChild) bottom.removeChild(bottom.firstChild)
      const actionsWrap = document.createElement('div')
      actionsWrap.className = 'feed-overlay-actions'

      if (!liveURL) {
        const shareBtn = document.createElement('button')
        shareBtn.className = 'feed-action-btn'
        shareBtn.type = 'button'
        shareBtn.setAttribute('data-feed-overlay-action', 'share')
        shareBtn.title = t('action_share', 'Share')
        shareBtn.setAttribute('aria-label', shareBtn.title)
        // Static SVG with no user input.
        shareBtn.innerHTML = getFeedActionIconSvg('share') // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(shareBtn)

        var liked = isQuote
          ? String(sourceCard.getAttribute('data-quote-liked') || '0') === '1'
          : stateBool(article, 'liked')
        var heartBtn = document.createElement('button')
        heartBtn.className = 'feed-action-btn' + (liked ? ' active' : '')
        heartBtn.type = 'button'
        heartBtn.setAttribute('data-feed-overlay-action', 'heart')
        heartBtn.title = liked ? t('action_unlike', 'Unlike') : t('action_like', 'Like')
        heartBtn.setAttribute('aria-label', heartBtn.title)
        // Static SVG with no user input.
        heartBtn.innerHTML = getFeedActionIconSvg('heart', liked) // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(heartBtn)

        var bookmarked = isQuote
          ? String(sourceCard.getAttribute('data-quote-bookmarked') || '0') === '1'
          : stateBool(article, 'bookmarked')
        var bmBtn = document.createElement('button')
        bmBtn.className = 'feed-action-btn' + (bookmarked ? ' active' : '')
        bmBtn.type = 'button'
        bmBtn.setAttribute('data-feed-overlay-action', 'bookmark')
        bmBtn.title = bookmarked ? t('action_unbookmark', 'Unbookmark') : t('action_bookmark', 'Bookmark')
        bmBtn.setAttribute('aria-label', bmBtn.title)
        // Static SVG with no user input.
        bmBtn.innerHTML = getFeedActionIconSvg('bookmark', bookmarked) // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(bmBtn)
      }

      var overlayLink = isQuote ? (quoteLink || link) : link
      var threadTweetId = isQuote ? quoteTweetId : tweetId
      if (threadTweetId) {
        var openThread = document.createElement('a')
        openThread.className = 'feed-action-btn'
        openThread.href = '/thread/' + encodeURIComponent(threadTweetId)
        openThread.title = t('feed_open_thread', 'Open thread')
        openThread.setAttribute('aria-label', openThread.title)
        openThread.innerHTML = getFeedActionIconSvg('thread') // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(openThread)
      }
      if (overlayLink) {
        var openExternal = document.createElement('a')
        openExternal.className = 'feed-action-btn'
        openExternal.href = overlayLink
        openExternal.target = '_blank'
        openExternal.rel = 'noopener noreferrer'
        openExternal.title = t('action_open_on_x', 'Open on X')
        openExternal.setAttribute('aria-label', openExternal.title)
        openExternal.setAttribute('data-feed-overlay-action', 'open')
        // Static SVG — no user input
        openExternal.innerHTML = getFeedActionIconSvg('open') // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(openExternal)
      }
      bottom.appendChild(actionsWrap)
      const sidebarPost = top.firstElementChild
      if (sidebarPost && !sidebarPost.querySelector(':scope > .feed-actions')) {
        const sidebarActions = actionsWrap.cloneNode(true)
        sidebarActions.className = 'feed-actions'
        sidebarPost.appendChild(sidebarActions)
      }
      syncFeedActionIcons(bottom)
    }
  }

  function renderVideo(activeStreamUrl, activePosterUrl, activePlaybackKind) {
    var videoWrap = document.createElement('div')
    videoWrap.className = 'feed-overlay-video-wrap'
    var fullscreenContent = document.createElement('div')
    fullscreenContent.setAttribute('data-fullscreen-content', '')
    videoWrap.appendChild(fullscreenContent)

    if (liveURL) {
      document.querySelectorAll('video[data-feed-inline-video]').forEach(function (video) { video.pause() })
    }
    var v = liveURL ? null : takeOverInlineVideoPlayback(overlay, activeStreamUrl)
    if (!v) {
      v = document.createElement('video')
      v.className = 'feed-overlay-video'
      v.autoplay = true
      v.playsInline = true
      v.loop = !liveURL
      if (activePosterUrl) v.poster = activePosterUrl
      if (!liveURL) {
        var source = document.createElement('source')
        source.src = activeStreamUrl
        source.type = 'video/mp4'
        v.appendChild(source)
      }
      v.muted = false
    }

    var togglePlayback = function (event) {
      event.stopPropagation()
      if (v && v._videoFeedback && typeof v._videoFeedback.markUserAction === 'function') {
        v._videoFeedback.markUserAction()
      }
      if (v.paused) v.play().catch(function () {}); else v.pause()
    }
    v.addEventListener('click', togglePlayback)
    overlay._overlayVideo = v
    overlay._overlayPlaybackKind = activePlaybackKind || 'video'
    overlay._videoClickHandler = togglePlayback

    fullscreenContent.appendChild(v)
    fullscreenContent.appendChild(createFeedVideoControls(liveURL ? { refresh: true, mini: false, cinema: false } : undefined))
    overlay._videoControlsCleanup = bindFeedVideoControls(videoWrap, v, {
      onCinema: closeMediaOverlay,
      onRefresh: function () {
        if (overlay._liveChatCleanup) overlay._liveChatCleanup()
        overlay._liveChatCleanup = null
        if (overlay._livePlayback) overlay._livePlayback.refresh()
      },
    })
    if (liveURL) startLivePlayback(v, videoWrap)
    return videoWrap
  }

  function startLivePlayback(video, videoWrap) {
    const status = document.createElement('span')
    status.className = 'feed-live-status'
    status.setAttribute('role', 'status')
    videoWrap.querySelector('[data-fullscreen-content]').appendChild(status)
    overlay._livePlayback = createXLivePlayback(video, liveURL, {
      onStatus: function (message) {
        status.textContent = message
        status.hidden = !message
      },
      onLive: function (live) {
        channelId = live.channel_id || ''
        channelName = live.display_name || live.handle || t('platform_x', 'X')
        article.setAttribute('data-channel-id', channelId)
        article.setAttribute('data-channel-name', channelName)
        article.setAttribute('data-author-handle', live.handle || '')
        article.setAttribute('data-avatar-url', live.avatar_url || '')
        article.setAttribute('data-feed-live-title', live.title || '')
        article.setAttribute('data-feed-live-status', live.live_status || '')
        const author = article.querySelector('.feed-author')
        if (author) author.textContent = live.display_name || live.handle || t('platform_x', 'X')
        const body = article.querySelector('.feed-body-text')
        if (body) body.textContent = live.title || ''
        const handle = article.querySelector('.feed-author-handle')
        if (handle) handle.textContent = live.handle ? '@' + live.handle : ''
        if (live.broadcast_id && !/\/i\/spaces\//.test(live.url || liveURL)) {
          overlay._liveChatCleanup = initBroadcastChat(video, liveChatPanel, '/api/x/lives/' + encodeURIComponent(live.broadcast_id) + '/chat')
        }
      },
    })
  }

  function render() {
    var activeSlide = (media.slides && media.slides[currentIndex]) || null
    var source = activeSlide ? activeSlide.source : 'parent'
    if (renderedSource !== source) {
      renderSidebar(source)
      renderedSource = source
    }
    if (!host) return
    restoreInlineVideoPlayback(overlay, false)
    disposeFeedImages(host)
    while (host.firstChild) host.removeChild(host.firstChild)
    overlay._overlayVideo = null
    var slideInfo = (media.kind === 'mixed' && media.slides && media.slides[currentIndex]) || null
    var isMixedVideo = slideInfo && slideInfo.kind === 'video' && slideInfo.streamUrl
    var isStandaloneVideo = !slideInfo && media.kind === 'video' && media.streamUrl
    var isVideo = isMixedVideo || isStandaloneVideo
    var activeStreamUrl = slideInfo ? slideInfo.streamUrl : media.streamUrl
    var activePosterUrl = slideInfo ? (slideInfo.posterUrl || slideInfo.url) : media.posterUrl
    var activePlaybackKind = slideInfo ? slideInfo.playbackKind : media.playbackKind

    if (isVideo) {
      host.appendChild(renderVideo(activeStreamUrl, activePosterUrl, activePlaybackKind))
    } else {
      const urls = Array.isArray(media.urls) ? media.urls : []
      const img = document.createElement('img')
      img.className = 'feed-overlay-image'
      img.alt = ''
      img.loading = 'eager'
      img.src = urls[currentIndex] || urls[0] || ''
      host.appendChild(img)
      initFeedImages(img)
    }
    const total = media.kind === 'video' ? 1 : (media.kind === 'mixed' && media.slides ? media.slides.length : Math.max(1, (media.urls || []).length))
    if (prev) prev.style.display = total > 1 ? '' : 'none'
    if (next) next.style.display = total > 1 ? '' : 'none'
  }

  function step(dir) {
    const total = media.kind === 'mixed' && media.slides ? media.slides.length : Math.max(1, (media.urls || []).length)
    if (media.kind === 'video' || total <= 1) return
    currentIndex = (currentIndex + dir + total) % total
    render()
  }

  overlay.addEventListener('click', function (event) {
    const mediaBackground = event.target.closest('.feed-media-overlay-left') &&
      event.target.matches('.feed-media-overlay-left, .feed-media-overlay-media, .feed-overlay-video-wrap, [data-fullscreen-content]')
    if (event.target === overlay || (!liveURL && mediaBackground)) {
      closeMediaOverlay()
      return
    }
    const closeBtn = event.target && event.target.closest ? event.target.closest('.feed-media-overlay-close') : null
    if (closeBtn) {
      event.preventDefault()
      closeMediaOverlay()
      return
    }
    const prevBtn = event.target && event.target.closest ? event.target.closest('.feed-media-overlay-nav.prev') : null
    if (prevBtn) { event.preventDefault(); step(-1); return }
    const nextBtn = event.target && event.target.closest ? event.target.closest('.feed-media-overlay-nav.next') : null
    if (nextBtn) { event.preventDefault(); step(1); return }
    const threadLink = event.target.closest('a[href^="/thread/"]')
    if (threadLink && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey) closeMediaOverlay()
  })
  if (prev) prev.addEventListener('click', function (event) { event.preventDefault(); event.stopPropagation(); step(-1) })
  if (next) next.addEventListener('click', function (event) { event.preventDefault(); event.stopPropagation(); step(1) })

  keyHandler = function (event) {
    if (event.defaultPrevented) return
    if (event.key === 'Tab' && overlay.contains(event.target)) {
      const controls = Array.from(overlay.querySelectorAll('button:not([disabled]), a[href], input:not([disabled]), [tabindex="0"]'))
        .filter(function (node) { return node.getClientRects().length > 0 })
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (event.shiftKey && event.target === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && event.target === last) {
        event.preventDefault()
        first.focus()
      }
      return
    }
    var target = event.target || {}
    var tag = String(target.tagName || '').toLowerCase()
    if (tag === 'input' || tag === 'textarea' || tag === 'select' || target.isContentEditable) return
    if (event.ctrlKey || event.altKey || event.metaKey) return
    if (event.key === 'Escape') {
      if (!(document.fullscreenElement || document.webkitFullscreenElement)) closeMediaOverlay()
      return
    }
    var activeVideo = overlay._overlayVideo
    var playbackKind = String(overlay._overlayPlaybackKind || 'video').toLowerCase()
    if (activeVideo && handleFeedVideoShortcut(event, activeVideo, { seek: playbackKind !== 'gif' })) {
      event.preventDefault()
      event.stopImmediatePropagation()
      return
    }
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      event.preventDefault()
      event.stopImmediatePropagation()
      step(event.key === 'ArrowLeft' ? -1 : 1)
    }
  }
  document.addEventListener('keydown', keyHandler)
  render()
  updatePanel()
  focusOverlay(overlay, triggerEl)
}

// ── Global bridge ──
// Other modules access the overlay element for like/bookmark sync and keyboard guards.

export function getOverlayElement() {
  return overlayEl
}

window.FeedMediaOverlay = {
  get element() { return overlayEl },
  get video() { return overlayEl && overlayEl._overlayVideo },
  open: openMediaOverlay,
  close: closeMediaOverlay,
}
