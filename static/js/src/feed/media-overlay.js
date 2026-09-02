// Media overlay module — extracted from feed_page.js
// Opens a fullscreen overlay with image/video media + tweet info sidebar.

import {
  itemRootFromNode,
  stateBool,
  getFeedActionIconSvg,
  syncFeedActionIcons,
  formatRelative,
  formatAbsolute,
  t,
} from '../utils.js'
import { bindFeedVideoControls, createFeedVideoControls, handleFeedVideoShortcut } from './video-controls.js'

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

function setFeedMediaOverlayOpen(open) {
  if (!document.body) return
  document.body.classList.toggle('feed-media-overlay-open', !!open)
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

  // No grid — look for a single .feed-media-wrap at this scope (not inside a nested quote card).
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
  if (overlayEl) {
    restoreInlineVideoPlayback(overlayEl, true)
    if (overlayEl.parentNode) overlayEl.remove()
  }
  overlayEl = null
  setFeedMediaOverlayOpen(false)
  if (keyHandler) {
    document.removeEventListener('keydown', keyHandler)
    keyHandler = null
  }
}

// ── Open overlay ──

export function openMediaOverlay(root, triggerEl) {
  if (window.matchMedia && window.matchMedia('(max-width: 768px)').matches) return
  const media = getMediaSources(root, triggerEl)
  if (!media) return
  closeMediaOverlay()
  const article = itemRootFromNode(root) || root
  const tweetId = String(article.getAttribute('data-tweet-id') || '').trim()
  const link = safeExternalHttpURL(article.getAttribute('data-feed-link'))
  const channelId = String(article.getAttribute('data-channel-id') || '').trim()
  const channelName = String(article.getAttribute('data-channel-name') || '').trim()
  const channelPlatform = String(article.getAttribute('data-channel-platform') || 'twitter').trim() || 'twitter'

  const triggerQuoteCard = triggerEl && triggerEl.closest ? triggerEl.closest('.feed-quote-card') : null
  const quoteCardEl = triggerQuoteCard || article.querySelector('.feed-quote-card')
  const isQuoteMedia = !!triggerQuoteCard
  const quoteTweetId = quoteCardEl ? String(quoteCardEl.getAttribute('data-quote-tweet-id') || '').trim() : ''
  const quoteLink = quoteCardEl ? safeExternalHttpURL(quoteCardEl.getAttribute('data-quote-link')) : ''

  let currentIndex = Math.max(0, Number(media.startIndex || 0))
  const overlay = document.createElement('div')
  overlay.className = 'feed-media-overlay'
  if (tweetId) overlay.setAttribute('data-feed-overlay-tweet-id', tweetId)
  // Quote overlay attribute is toggled per slide by renderSidebar.
  // Static overlay shell template — no user input
  overlay.innerHTML = '' + // eslint-disable-line no-unsanitized/property
    '<div class="feed-media-overlay-shell">' +
    '<button class="feed-media-overlay-close" type="button" aria-label="' + t('action_close', 'Close') + '">\u00d7</button>' +
    '<div class="feed-media-overlay-main">' +
    '<div class="feed-media-overlay-left">' +
    '<button class="feed-media-overlay-nav prev" type="button" aria-label="' + t('action_previous', 'Previous') + '">\u2039</button>' +
    '<div class="feed-media-overlay-media"></div>' +
    '<button class="feed-media-overlay-nav next" type="button" aria-label="' + t('action_next', 'Next') + '">\u203a</button>' +
    '<div class="feed-media-overlay-counter"></div>' +
    '</div>' +
    '<div class="feed-media-overlay-right">' +
    '<div class="feed-media-overlay-top"></div>' +
    '<div class="feed-media-overlay-bottom"></div>' +
    '</div>' +
    '</div>' +
    '</div>'
  document.body.appendChild(overlay)
  overlayEl = overlay
  setFeedMediaOverlayOpen(true)

  const top = overlay.querySelector('.feed-media-overlay-top')
  const host = overlay.querySelector('.feed-media-overlay-media')
  const counter = overlay.querySelector('.feed-media-overlay-counter')
  const prev = overlay.querySelector('.feed-media-overlay-nav.prev')
  const next = overlay.querySelector('.feed-media-overlay-nav.next')
  const bottom = overlay.querySelector('.feed-media-overlay-bottom')

  function renderSidebar(sourceKind) {
    const isQuote = sourceKind === 'quote' && !!quoteCardEl
    const sourceCard = isQuote ? quoteCardEl : article

    // ── Ownership attribute — downstream handlers in feed/index.js read this.
    if (isQuote && quoteTweetId) {
      overlay.setAttribute('data-feed-overlay-quote-tweet-id', quoteTweetId)
    } else {
      overlay.removeAttribute('data-feed-overlay-quote-tweet-id')
    }

    // ── Top (author / body / date / header actions) ──
    if (top) {
      while (top.firstChild) top.removeChild(top.firstChild)
      top.classList.add('channel-row')

      var authorLabel, authorHandleRaw, showAuthorHandle, dateText, dateAbsolute, bodySourceEl, titleText, summaryText, repostText
      var overlayChannelId = channelId

      if (isQuote) {
        authorLabel = textContentTrim(sourceCard.querySelector('.feed-quote-author')) || t('feed_quoted_post', 'Quoted post')
        authorHandleRaw = textContentTrim(sourceCard.querySelector('.feed-author-handle')).replace(/^@+/, '')
        overlayChannelId = 'twitter_' + authorHandleRaw
        bodySourceEl = sourceCard.querySelector('.feed-quote-text')
        dateText = ''
        dateAbsolute = ''
        titleText = ''
        summaryText = ''
        repostText = ''
      } else {
        authorLabel = textContentTrim(article.querySelector('.feed-author'))
          || String(article.getAttribute('data-feed-author') || '').trim()
          || channelName
          || t('feed_x_post', 'X post')
        authorHandleRaw = textContentTrim(article.querySelector('.feed-author-handle')).replace(/^@+/, '')
        var dateEl = article.querySelector('.feed-date-inline')
        var dateRaw = String((dateEl && dateEl.getAttribute('data-feed-date-raw')) || article.getAttribute('data-feed-date') || '').trim()
        dateText = textContentTrim(dateEl).replace(/^·\s*/, '') || formatRelative(dateRaw) || dateRaw
        dateAbsolute = formatAbsolute(dateRaw)
        bodySourceEl = article.querySelector('.feed-body-text')
        titleText = textContentTrim(article.querySelector('.feed-text'))
        summaryText = textContentTrim(article.querySelector('.feed-summary'))
        repostText = textContentTrim(article.querySelector('.feed-repost-line'))
      }
      showAuthorHandle = !!(authorHandleRaw && authorLabel.toLowerCase() !== authorHandleRaw.toLowerCase())

      if (overlayChannelId) top.setAttribute('data-channel-id', overlayChannelId)
      if (channelPlatform) top.setAttribute('data-channel-platform', channelPlatform)

      if (repostText) {
        const repostEl = document.createElement('div')
        repostEl.className = 'feed-overlay-repost'
        repostEl.textContent = repostText
        top.appendChild(repostEl)
      }

      var headlineId = isQuote ? overlayChannelId : channelId
      const headline = document.createElement((headlineId || link) ? 'a' : 'div')
      headline.className = 'feed-overlay-headline'
      if (headline.tagName === 'A') {
        headline.href = headlineId ? ('/channels/' + encodeURIComponent(headlineId)) : link
        if (!headlineId && link) {
          headline.target = '_blank'
          headline.rel = 'noopener noreferrer'
        }
      }
      if (headlineId) headline.setAttribute('data-feed-channel-id', headlineId)
      var avatarSource = isQuote ? sourceCard.querySelector('.feed-quote-avatar') : article.querySelector('.feed-avatar')
      if (avatarSource) {
        var avatarClone = avatarSource.cloneNode(true)
        avatarClone.className = 'feed-avatar'
        headline.appendChild(avatarClone)
      } else {
        const fallbackAvatar = document.createElement('div')
        fallbackAvatar.className = 'feed-avatar'
        const fallbackChar = document.createElement('span')
        fallbackChar.className = 'feed-avatar-fallback'
        fallbackChar.textContent = (authorLabel.charAt(0) || 'X').toUpperCase()
        fallbackAvatar.appendChild(fallbackChar)
        headline.appendChild(fallbackAvatar)
      }
      const authorMeta = document.createElement('div')
      authorMeta.className = 'feed-overlay-author-meta'
      const authorEl = document.createElement('div')
      authorEl.className = 'feed-overlay-author'
      authorEl.textContent = authorLabel
      authorMeta.appendChild(authorEl)
      var subLine = document.createElement('div')
      subLine.className = 'feed-overlay-sub'
      if (showAuthorHandle) {
        var handleLink = document.createElement('a')
        handleLink.className = 'feed-author-handle feed-inline-link'
        handleLink.href = '/channels/' + encodeURIComponent(overlayChannelId)
        handleLink.textContent = '@' + authorHandleRaw
        subLine.appendChild(handleLink)
      }
      if (dateText) {
        if (showAuthorHandle) {
          subLine.appendChild(document.createTextNode(' \u00b7 '))
        }
        var dateSpan = document.createElement('span')
        dateSpan.className = 'feed-overlay-date'
        dateSpan.textContent = dateText
        if (dateAbsolute && dateAbsolute !== dateText) dateSpan.title = dateAbsolute
        subLine.appendChild(dateSpan)
      }
      if (subLine.childNodes.length) authorMeta.appendChild(subLine)
      headline.appendChild(authorMeta)

      if (!isQuote) {
        var headerActions = article.querySelector('.feed-header-actions')
        if (headerActions) {
          var actionsClone = headerActions.cloneNode(true)
          actionsClone.classList.add('feed-overlay-header-actions')
          headline.appendChild(actionsClone)
        }
      } else {
        var qFollowBtn = sourceCard ? sourceCard.querySelector('.feed-quote-follow-btn') : null
        if (qFollowBtn) {
          var qActionsWrap = document.createElement('div')
          qActionsWrap.className = 'feed-header-actions feed-overlay-header-actions'
          qActionsWrap.appendChild(qFollowBtn.cloneNode(true))
          headline.appendChild(qActionsWrap)
        }
      }
      top.appendChild(headline)

      if (bodySourceEl && textContentTrim(bodySourceEl)) {
        const bodyEl = document.createElement('p')
        bodyEl.className = 'feed-overlay-text'
        // Clone children to preserve @mention links and other HTML
        var childNodes = bodySourceEl.childNodes
        for (var ci = 0; ci < childNodes.length; ci++) {
          bodyEl.appendChild(childNodes[ci].cloneNode(true))
        }
        top.appendChild(bodyEl)
      } else {
        if (titleText) {
          const titleEl = document.createElement('p')
          titleEl.className = 'feed-overlay-text'
          titleEl.textContent = titleText
          top.appendChild(titleEl)
        }
        if (summaryText) {
          const summaryEl = document.createElement('p')
          summaryEl.className = 'feed-overlay-summary'
          summaryEl.textContent = summaryText
          top.appendChild(summaryEl)
        }
      }

      updateRetweetMenuLabels(top)
      syncFeedActionIcons(top)
    }

    // ── Bottom (share / heart / bookmark / open-on-X) ──
    if (bottom) {
      while (bottom.firstChild) bottom.removeChild(bottom.firstChild)
      const actionsWrap = document.createElement('div')
      actionsWrap.className = 'feed-overlay-actions'

      const shareBtn = document.createElement('button')
      shareBtn.className = 'feed-action-btn'
      shareBtn.type = 'button'
      shareBtn.setAttribute('data-feed-overlay-action', 'share')
      shareBtn.title = t('action_copy_link', 'Copy link')
      shareBtn.setAttribute('aria-label', shareBtn.title)
      // Static SVG — no user input
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
      // Static SVG — no user input
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
      // Static SVG — no user input
      bmBtn.innerHTML = getFeedActionIconSvg('bookmark', bookmarked) // eslint-disable-line no-unsanitized/property
      actionsWrap.appendChild(bmBtn)

      var overlayLink = isQuote ? (quoteLink || link) : link
      if (overlayLink) {
        var openX = document.createElement('a')
        openX.className = 'feed-action-btn'
        openX.href = overlayLink
        openX.target = '_blank'
        openX.rel = 'noopener noreferrer'
        openX.title = t('action_open_on_x', 'Open on X')
        openX.setAttribute('aria-label', openX.title)
        openX.setAttribute('data-feed-overlay-action', 'openx')
        // Static SVG — no user input
        openX.innerHTML = getFeedActionIconSvg('link') // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(openX)
      }
      var threadHref = String(article.getAttribute('data-feed-thread-href') || '').trim()
      if (threadHref.startsWith('/') && !threadHref.startsWith('//')) {
        var openPost = document.createElement('a')
        openPost.className = 'feed-action-btn'
        openPost.href = threadHref
        openPost.title = t('profile_open_post', 'Open post')
        openPost.setAttribute('aria-label', openPost.title)
        // Static SVG — no user input
        openPost.innerHTML = getFeedActionIconSvg('open') // eslint-disable-line no-unsanitized/property
        actionsWrap.appendChild(openPost)
      }
      bottom.appendChild(actionsWrap)
      syncFeedActionIcons(bottom)
    }
  }

  function renderVideo(activeStreamUrl, activePosterUrl, activePlaybackKind) {
    var videoWrap = document.createElement('div')
    videoWrap.className = 'feed-overlay-video-wrap'

    var v = takeOverInlineVideoPlayback(overlay, activeStreamUrl)
    if (!v) {
      v = document.createElement('video')
      v.className = 'feed-overlay-video'
      v.autoplay = true
      v.playsInline = true
      v.loop = true
      if (activePosterUrl) v.poster = activePosterUrl
      var source = document.createElement('source')
      source.src = activeStreamUrl
      source.type = 'video/mp4'
      v.appendChild(source)
      v.muted = false
    }

    var togglePlayback = function (event) {
      event.stopPropagation()
      if (v.paused) v.play().catch(function () {}); else v.pause()
    }
    v.addEventListener('click', togglePlayback)
    overlay._overlayVideo = v
    overlay._overlayPlaybackKind = activePlaybackKind || 'video'
    overlay._videoClickHandler = togglePlayback

    videoWrap.appendChild(v)
    videoWrap.appendChild(createFeedVideoControls())
    overlay._videoControlsCleanup = bindFeedVideoControls(videoWrap, v, {
      onCinema: closeMediaOverlay,
    })
    return videoWrap
  }

  function render() {
    var activeSlide = (media.slides && media.slides[currentIndex]) || null
    var activeSource = activeSlide ? activeSlide.source : (isQuoteMedia ? 'quote' : 'parent')
    renderSidebar(activeSource)
    if (!host) return
    restoreInlineVideoPlayback(overlay, false)
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
    }
    const total = media.kind === 'video' ? 1 : (media.kind === 'mixed' && media.slides ? media.slides.length : Math.max(1, (media.urls || []).length))
    if (counter) counter.textContent = total > 1 ? (String(currentIndex + 1) + ' / ' + String(total)) : ''
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
    if (event.target === overlay) closeMediaOverlay()
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
  })
  if (prev) prev.addEventListener('click', function (event) { event.preventDefault(); event.stopPropagation(); step(-1) })
  if (next) next.addEventListener('click', function (event) { event.preventDefault(); event.stopPropagation(); step(1) })

  keyHandler = function (event) {
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
    if (event.key === 'ArrowLeft') step(-1)
    else if (event.key === 'ArrowRight') step(1)
  }
  document.addEventListener('keydown', keyHandler)
  render()
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
