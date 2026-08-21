const SHELL_WIDTH_KEY = 'igloo.mini-player.width.v2'
const LEGACY_SHELL_WIDTH_KEY = 'igloo.mini-player.width.v1'
const SHELL_MIN_WIDTH = 300
const SHELL_MAX_WIDTH = 1040

export function preferredShellWidth(savedValue, legacyValue, viewportWidth) {
  const saved = Number(savedValue || 0)
  const legacy = Number(legacyValue || 0)
  const width = saved >= 300 && saved <= SHELL_MAX_WIDTH
    ? saved
    : legacy >= 300 && legacy <= 800
      ? legacy * 1.5
      : 0
  if (!width) return 0
  return Math.min(width, SHELL_MAX_WIDTH, Math.max(0, Number(viewportWidth || 0) - 16))
}

export function resizedShellWidth(direction, startWidth, startHeight, deltaX, deltaY) {
  const hasHorizontalEdge = direction.includes('left') || direction.includes('right')
  const hasVerticalEdge = direction.includes('top') || direction.includes('bottom')
  const horizontal = direction.includes('left') ? -deltaX : deltaX
  if (!hasVerticalEdge) return startWidth + horizontal
  const aspectRatio = startHeight > 0 ? startWidth / startHeight : 1
  const vertical = (direction.includes('top') ? -deltaY : deltaY) * aspectRatio
  if (!hasHorizontalEdge) return startWidth + vertical
  return startWidth + (Math.abs(horizontal) >= Math.abs(vertical) ? horizontal : vertical)
}

export function reconnectMediaController(surface) {
  const controller = surface && surface.querySelector ? surface.querySelector('media-controller') : null
  if (!controller || typeof controller.associateElement !== 'function') return false
  controller.associateElement(controller)
  return true
}

export function feedThreadURL(tweetID) {
  const id = String(tweetID || '').trim()
  return id ? '/thread/' + encodeURIComponent(id) : ''
}

export function shouldDismissForYouTube(activeKind, activeVideo, nextVideo) {
  return activeKind === 'videos' && !!activeVideo && !!nextVideo && activeVideo !== nextVideo
}

export function canAutomaticallyDockVideo(video) {
  return !!video && !video.ended && (!video.paused || Number(video.currentTime || 0) > 0)
}

export function isPlayerURL(value, base) {
  try {
    return /^\/player\/[^/]+$/.test(new URL(String(value || ''), base || 'https://igloo.invalid/').pathname)
  } catch (_) {
    return false
  }
}

function normalizedMediaURL(value) {
  try {
    const parsed = new URL(String(value || ''), 'https://igloo.invalid/')
    return parsed.pathname + parsed.search
  } catch (_) {
    return String(value || '')
  }
}

export function findFeedThreadVideo(ownerDocument, tweetID, streamURL) {
  if (!ownerDocument || !tweetID) return null
  const cards = ownerDocument.querySelectorAll('[data-feed-item][data-tweet-id]')
  let card = null
  for (let index = 0; index < cards.length; index += 1) {
    if (String(cards[index].getAttribute('data-tweet-id') || '') === tweetID) {
      card = cards[index]
      break
    }
  }
  if (!card) return null
  const candidates = card.querySelectorAll('[data-feed-media-kind="video"]')
  let fallback = null
  for (let index = 0; index < candidates.length; index += 1) {
    const candidate = candidates[index]
    if (!candidate.querySelector('video')) continue
    if (!fallback) fallback = candidate
    if (streamURL && normalizedMediaURL(candidate.getAttribute('data-feed-media-stream')) === normalizedMediaURL(streamURL)) return candidate
  }
  return fallback
}

export function shouldAutomaticallyMini(kind, preferences) {
  const prefs = preferences || {}
  if (kind === 'feed') return prefs.miniPlayerFeedEnabled === true
  return prefs.miniPlayerVideosEnabled === true
}

export function feedAccountName(wrap) {
  if (!wrap || !wrap.closest) return ''
  const card = wrap.closest('[data-feed-item]')
  const overlay = wrap.closest('.feed-media-overlay')
  const author = card && card.querySelector
    ? card.querySelector('.feed-author, .feed-quote-author')
    : overlay && overlay.querySelector
      ? overlay.querySelector('.feed-overlay-author')
      : null
  return String(author && author.textContent || '').trim()
}

function initMiniPlayer() {
  if (window.top !== window.self) {
    initFramePlayerBridge()
    return
  }

  const doc = document
  const shell = doc.getElementById('mini-player-shell')
  const mediaHost = doc.getElementById('mini-player-media-host')
  const shellTitle = doc.getElementById('mini-player-title')
  const returnButton = doc.getElementById('mini-player-return')
  const closeButton = doc.getElementById('mini-player-close')
  const resizeHandle = doc.getElementById('mini-player-resize-handle')
  const resizeTargets = Array.from(doc.querySelectorAll('[data-mini-player-resize]'))
  let browseFrame = doc.getElementById('mini-player-browse-frame')
  const app = doc.querySelector('.app')

  if (!shell || !mediaHost || !browseFrame || !app) return

  const originalDocumentTitle = doc.title
  let activeSurface = null
  let pendingFeedThreadReturn = null
  let browseActive = false
  let browseURL = ''

  function relativeURL(value, base) {
    const parsed = new URL(String(value || '/'), base || window.location.href)
    return parsed.pathname + parsed.search + parsed.hash
  }

  function documentURL(ownerDocument) {
    try {
      const location = ownerDocument.defaultView.location
      return location.pathname + location.search + location.hash
    } catch (_) {
      return window.location.pathname + window.location.search + window.location.hash
    }
  }

  function isEligibleURL(value, base) {
    let target
    try {
      target = new URL(String(value || ''), base || window.location.href)
    } catch (_) {
      return false
    }
    if (target.origin !== window.location.origin) return false
    if (/^\/(?:api|static)(?:\/|$)/.test(target.pathname)) return false
    if (/^\/(?:login|logout|setup)(?:\/|$)/.test(target.pathname)) return false
    return true
  }

  function linkForClick(event, ownerDocument) {
    if (event.defaultPrevented || event.button !== 0) return null
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return null
    const anchor = event.target && event.target.closest ? event.target.closest('a[href]') : null
    if (!anchor || anchor.ownerDocument !== ownerDocument) return null
    if (anchor.hasAttribute('download')) return null
    if (anchor.matches('[hx-get], [hx-post], [hx-put], [hx-patch], [hx-delete]')) return null
    const target = String(anchor.getAttribute('target') || '').toLowerCase()
    if (target && target !== '_self') return null
    const href = anchor.getAttribute('href') || ''
    if (!href || href[0] === '#') return null
    return anchor
  }

  function preferences() {
    return window.IglooPreferences || {}
  }

  function feedSurface(wrap, video) {
    if (!wrap || !video) return null
    const title = feedAccountName(wrap)
    const button = wrap.querySelector ? wrap.querySelector('[data-feed-video-mini]') : null
    return {
      element: wrap,
      video: video,
      button: button,
      title: title || String(button && button.getAttribute('aria-label') || '').trim() || wrap.ownerDocument.title,
      kind: 'feed',
    }
  }

  function feedReturnDetails(wrap, video) {
    const overlay = wrap && wrap.closest ? wrap.closest('.feed-media-overlay') : null
    const quoteCard = wrap && wrap.closest ? wrap.closest('.feed-quote-card') : null
    const card = wrap && wrap.closest ? wrap.closest('[data-feed-item]') : null
    const tweetID = String(
      overlay && overlay.getAttribute('data-feed-overlay-quote-tweet-id') ||
      overlay && overlay.getAttribute('data-feed-overlay-tweet-id') ||
      quoteCard && quoteCard.getAttribute('data-quote-tweet-id') ||
      card && card.getAttribute('data-tweet-id') ||
      ''
    ).trim()
    const source = video && video.querySelector ? video.querySelector('source') : null
    const streamURL = String(
      wrap && wrap.getAttribute && wrap.getAttribute('data-feed-media-stream') ||
      video && (video.currentSrc || video.getAttribute && video.getAttribute('src')) ||
      source && source.getAttribute('src') ||
      ''
    ).trim()
    return {
      feedTweetID: tweetID,
      feedStreamURL: streamURL,
      threadURL: feedThreadURL(tweetID),
    }
  }

  function youtubeSurface(ownerDocument) {
    const root = ownerDocument.getElementById('player-root')
    const wrapper = root && root.querySelector('.player-wrapper')
    const video = ownerDocument.getElementById('mpa-player')
    const button = ownerDocument.getElementById('player-mini-btn')
    if (!root || !wrapper || !video || !button) return null
    if ((root.getAttribute('data-channel-platform') || '').toLowerCase() !== 'youtube') return null
    const titleElement = ownerDocument.getElementById('player-title')
    return {
      element: wrapper,
      video: video,
      button: button,
      title: String(titleElement && titleElement.textContent || '').trim() || ownerDocument.title,
      kind: 'videos',
    }
  }

  function playingSurface(ownerDocument) {
    const youtube = youtubeSurface(ownerDocument)
    if (youtube && canAutomaticallyDockVideo(youtube.video)) return youtube

    const videos = ownerDocument.querySelectorAll('[data-feed-media-kind="video"] video, .feed-overlay-video-wrap video')
    let started = null
    for (let index = 0; index < videos.length; index += 1) {
      const video = videos[index]
      if (!canAutomaticallyDockVideo(video)) continue
      const wrap = video.closest('[data-feed-media], .feed-overlay-video-wrap')
      if (!wrap) continue
      const surface = feedSurface(wrap, video)
      if (!video.paused) return surface
      if (!started) started = surface
    }
    return started
  }

  function normalizeSurface(value) {
    if (!value || !value.element || !value.video) return null
    const sourceDocument = value.sourceDocument || value.element.ownerDocument
    const kind = value.kind === 'feed' ? 'feed' : 'videos'
    const feedReturn = kind === 'feed' ? feedReturnDetails(value.element, value.video) : {}
    const feedTitle = kind === 'feed' ? feedAccountName(value.element) : ''
    return {
      element: value.element,
      video: value.video,
      button: value.button || null,
      title: String(value.title || feedTitle || sourceDocument.title || 'Mini player').trim(),
      kind: kind,
      sourceDocument: sourceDocument,
      homeURL: value.homeURL || documentURL(sourceDocument),
      feedTweetID: value.feedTweetID || feedReturn.feedTweetID || '',
      feedStreamURL: value.feedStreamURL || feedReturn.feedStreamURL || '',
      threadURL: value.threadURL || feedReturn.threadURL || '',
      placeholder: null,
      sourceFrame: null,
    }
  }

  function sameDocumentURL(ownerDocument, value) {
    if (!ownerDocument || !value) return false
    try {
      const current = new URL(documentURL(ownerDocument), window.location.origin)
      const target = new URL(value, window.location.origin)
      return current.pathname === target.pathname
    } catch (_) {
      return false
    }
  }

  function sameURLPath(left, right) {
    try {
      return new URL(left, window.location.origin).pathname === new URL(right, window.location.origin).pathname
    } catch (_) {
      return false
    }
  }

  function sameSurfaceHome(value, surface) {
    const current = surface || activeSurface
    if (!current || !current.homeURL) return false
    try {
      const target = new URL(String(value || '/'), window.location.href)
      const home = new URL(current.homeURL, window.location.origin)
      return target.origin === home.origin && target.pathname === home.pathname && target.search === home.search
    } catch (_) {
      return false
    }
  }

  function setAppBrowsing(active) {
    if (active) {
      app.setAttribute('inert', '')
      app.setAttribute('aria-hidden', 'true')
    } else {
      app.removeAttribute('inert')
      app.removeAttribute('aria-hidden')
    }
    doc.body.classList.toggle('mini-player-browsing', active)
  }

  function setSurfaceButton(surface, active) {
    const button = surface && surface.button
    if (!button || !button.classList) return
    button.classList.toggle('active', active)
    button.setAttribute('aria-pressed', active ? 'true' : 'false')
  }

  function makePlaceholder(surface) {
    const placeholder = surface.sourceDocument.createElement('div')
    placeholder.className = surface.kind === 'feed' ? 'feed-mini-placeholder' : 'player-mini-placeholder'
    if (surface.kind === 'feed' && surface.element.getBoundingClientRect) {
      const rect = surface.element.getBoundingClientRect()
      if (rect.height > 0) placeholder.style.height = Math.round(rect.height) + 'px'
    }
    return placeholder
  }

  function clearActiveSurface(surface) {
    setSurfaceButton(surface, false)
    activeSurface = null
    pendingFeedThreadReturn = null
    shell.classList.add('hidden')
    shell.setAttribute('aria-hidden', 'true')
    if (shellTitle) shellTitle.textContent = ''
  }

  function discardSourceFrame(surface) {
    const sourceFrame = surface && surface.sourceFrame
    if (!sourceFrame || sourceFrame === browseFrame) return
    sourceFrame.remove()
    surface.sourceFrame = null
  }

  function restoreActiveSurface(options) {
    const opts = options || {}
    const surface = activeSurface
    if (!surface) return null
    if (opts.pause === true) {
      try { surface.video.pause() } catch (_) {}
    }

    if (surface.placeholder && surface.placeholder.isConnected) {
      surface.placeholder.replaceWith(surface.element)
      reconnectMediaController(surface.element)
    } else if (surface.element.parentNode === mediaHost) {
      surface.element.remove()
    }
    if (surface.placeholder && surface.placeholder.isConnected) surface.placeholder.remove()
    surface.placeholder = null
    clearActiveSurface(surface)

    if (opts.focus === true && surface.button && surface.button.isConnected) {
      try { surface.button.focus() } catch (_) {}
    }
    if (opts.keepSourceFrame !== true) discardSourceFrame(surface)
    return surface
  }

  function feedReturnTarget(ownerDocument, surface) {
    if (!surface) return null
    return findFeedThreadVideo(ownerDocument, surface.feedTweetID, surface.feedStreamURL)
  }

  function finishFeedThreadReturn(ownerDocument) {
    const surface = pendingFeedThreadReturn
    if (!surface || surface !== activeSurface) return false
    const target = feedReturnTarget(ownerDocument, surface)
    if (!target) return false

    target.replaceWith(surface.element)
    reconnectMediaController(surface.element)
    if (surface.placeholder && surface.placeholder.isConnected) surface.placeholder.remove()
    surface.placeholder = null
    if (surface.element.classList && surface.element.classList.contains('feed-overlay-video-wrap')) {
      surface.element.classList.add('feed-returned-video-wrap')
    }
    clearActiveSurface(surface)
    discardSourceFrame(surface)
    try { surface.element.scrollIntoView({ block: 'center' }) } catch (_) {}
    if (surface.button && surface.button.isConnected) {
      try { surface.button.focus() } catch (_) {}
    }
    return true
  }

  function dockSurface(value) {
    const next = normalizeSurface(value)
    if (!next) return false
    if (activeSurface && activeSurface.element === next.element) return true
    if (activeSurface) restoreActiveSurface({ pause: true })
    pendingFeedThreadReturn = null

    next.placeholder = makePlaceholder(next)
    next.element.before(next.placeholder)
    shell.style.left = ''
    shell.style.top = ''
    shell.style.right = ''
    shell.style.bottom = ''
    mediaHost.appendChild(next.element)
    reconnectMediaController(next.element)
    activeSurface = next
    shell.classList.remove('hidden')
    shell.setAttribute('aria-hidden', 'false')
    syncResizeHandle(shell.getBoundingClientRect().width)
    setSurfaceButton(next, true)
    if (shellTitle) shellTitle.textContent = next.title
    return true
  }

  function replaceFrameLocation(value) {
    browseURL = relativeURL(value)
    try {
      browseFrame.contentWindow.location.replace(browseURL)
    } catch (_) {
      browseFrame.src = browseURL
    }
  }

  function createBrowseFrame(title) {
    const frame = doc.createElement('iframe')
    frame.id = 'mini-player-browse-frame'
    frame.className = 'mini-player-browse-frame hidden'
    frame.title = title || 'Igloo browsing view'
    frame.setAttribute('aria-hidden', 'true')
    bindBrowseFrame(frame)
    return frame
  }

  function retainActiveSourceFrame() {
    if (!activeSurface) return
    try {
      if (activeSurface.sourceDocument !== browseFrame.contentDocument) return
    } catch (_) {
      return
    }

    const retained = browseFrame
    activeSurface.sourceFrame = retained
    retained.removeAttribute('id')
    retained.setAttribute('data-mini-player-source-frame', '1')
    retained.classList.add('mini-player-source-frame')
    retained.classList.remove('hidden')
    retained.setAttribute('inert', '')
    retained.setAttribute('aria-hidden', 'true')

    const next = createBrowseFrame(retained.title)
    retained.after(next)
    browseFrame = next
  }

  function activateSourceFrame(surface) {
    const sourceFrame = surface && surface.sourceFrame
    if (!sourceFrame || sourceFrame === browseFrame) return false
    const replaced = browseFrame
    browseFrame = sourceFrame
    sourceFrame.id = 'mini-player-browse-frame'
    sourceFrame.removeAttribute('data-mini-player-source-frame')
    sourceFrame.removeAttribute('inert')
    sourceFrame.classList.remove('mini-player-source-frame')
    sourceFrame.classList.remove('hidden')
    sourceFrame.setAttribute('aria-hidden', 'false')
    surface.sourceFrame = null
    replaced.remove()
    browseActive = true
    browseURL = surface.homeURL
    setAppBrowsing(true)
    try {
      const sourceTitle = sourceFrame.contentDocument && sourceFrame.contentDocument.title
      if (sourceTitle) doc.title = sourceTitle
    } catch (_) {}
    return true
  }

  function navigateBrowse(value, options) {
    const opts = options || {}
    retainActiveSourceFrame()
    browseURL = relativeURL(value)
    browseActive = true
    browseFrame.classList.remove('hidden')
    browseFrame.setAttribute('aria-hidden', 'false')
    setAppBrowsing(true)
    if (opts.push !== false) {
      window.history.pushState({
        iglooMiniBrowse: true,
        iglooMiniSurfaceHome: activeSurface && activeSurface.homeURL || '',
      }, '', browseURL)
    }
    replaceFrameLocation(browseURL)
  }

  function returnToSurface(options) {
    const opts = options || {}
    const surface = activeSurface
    if (!surface) return
    if (surface.kind === 'feed' && surface.threadURL && !sameURLPath(surface.homeURL, surface.threadURL)) {
      pendingFeedThreadReturn = surface
      if (browseActive) {
        try {
          if (sameDocumentURL(browseFrame.contentDocument, surface.threadURL) && finishFeedThreadReturn(browseFrame.contentDocument)) return
        } catch (_) {}
      }
      navigateBrowse(surface.threadURL)
      return
    }
    if (surface.sourceFrame && surface.sourceFrame !== browseFrame) {
      const restored = restoreActiveSurface({ keepSourceFrame: true })
      activateSourceFrame(restored)
      if (restored && opts.push !== false) {
        window.history.pushState({ iglooMiniSurfaceHome: restored.homeURL }, '', restored.homeURL)
      }
      if (restored && restored.button && restored.button.isConnected) {
        try { restored.button.focus() } catch (_) {}
      }
      try { restored && restored.element.scrollIntoView({ block: 'start' }) } catch (_) {}
      return
    }
    let sourceIsBrowseDocument = false
    try { sourceIsBrowseDocument = browseActive && surface.sourceDocument === browseFrame.contentDocument } catch (_) {}

    if (browseActive && !sourceIsBrowseDocument) {
      browseFrame.classList.add('hidden')
      browseFrame.setAttribute('aria-hidden', 'true')
      setAppBrowsing(false)
      browseActive = false
      browseURL = ''
      try { browseFrame.contentWindow.location.replace('about:blank') } catch (_) { browseFrame.src = 'about:blank' }
      doc.title = originalDocumentTitle
    }

    const restored = restoreActiveSurface({ focus: true })
    if (restored && opts.push !== false && !sourceIsBrowseDocument) {
      window.history.pushState({ iglooMiniSurfaceHome: restored.homeURL }, '', restored.homeURL)
    }
    try { restored && restored.element.scrollIntoView({ block: 'start' }) } catch (_) {}
  }

  function closeMiniPlayer() {
    if (!activeSurface) return
    restoreActiveSurface({ pause: true })
  }

  function leaveMiniPlayerForPlayer(value) {
    if (activeSurface) restoreActiveSurface({ pause: true })
    window.location.assign(value)
  }

  function handleYouTubePlayback(video) {
    if (!activeSurface || !shouldDismissForYouTube(activeSurface.kind, activeSurface.video, video)) return
    restoreActiveSurface({ pause: true })
  }

  function installFrameNavigation() {
    if (!browseActive) return
    let frameDocument
    let frameWindow
    try {
      frameDocument = browseFrame.contentDocument
      frameWindow = browseFrame.contentWindow
      if (!frameDocument || !frameWindow || frameWindow.location.origin !== window.location.origin) return
    } catch (_) {
      return
    }

    const current = frameWindow.location.pathname + frameWindow.location.search + frameWindow.location.hash
    if (current && current !== 'about:blank') {
      browseURL = current
      window.history.replaceState({
        iglooMiniBrowse: true,
        iglooMiniSurfaceHome: activeSurface && activeSurface.homeURL || '',
      }, '', current)
      if (frameDocument.title) doc.title = frameDocument.title
    }
    finishFeedThreadReturn(frameDocument)
    if (frameDocument.documentElement.dataset.iglooMiniNavigationBound === '1') return
    frameDocument.documentElement.dataset.iglooMiniNavigationBound = '1'

    frameDocument.addEventListener('play', function (event) {
      const video = event.target
      const root = video && video.closest ? video.closest('#player-root') : null
      if (!root || String(root.getAttribute('data-channel-platform') || '').toLowerCase() !== 'youtube') return
      handleYouTubePlayback(video)
    }, true)

    frameDocument.addEventListener('click', function (event) {
      if (!browseActive) return
      const anchor = linkForClick(event, frameDocument)
      if (!anchor) return
      const target = new URL(anchor.href, frameWindow.location.href)
      if (target.origin !== window.location.origin) return
      if (/^\/(?:login|logout|setup)(?:\/|$)/.test(target.pathname)) {
        event.preventDefault()
        window.location.assign(target.href)
        return
      }
      if (!isEligibleURL(target.href, frameWindow.location.href)) return
      event.preventDefault()

      if (activeSurface && sameSurfaceHome(target.href)) {
        returnToSurface()
        return
      }
      if (activeSurface && activeSurface.kind === 'videos' && isPlayerURL(target.href, frameWindow.location.href)) {
        leaveMiniPlayerForPlayer(target.href)
        return
      }
      if (!activeSurface) {
        const candidate = playingSurface(frameDocument)
        if (candidate && shouldAutomaticallyMini(candidate.kind, preferences())) dockSurface(candidate)
      }
      navigateBrowse(target.href)
    })

    frameDocument.addEventListener('submit', function (event) {
      if (!browseActive) return
      const form = event.target
      if (!form || String(form.method || 'get').toLowerCase() !== 'get') return
      if (form.hasAttribute('hx-get') || form.hasAttribute('hx-post')) return
      const target = new URL(form.action || frameWindow.location.href, frameWindow.location.href)
      if (target.origin !== window.location.origin || !isEligibleURL(target.href, frameWindow.location.href)) return
      event.preventDefault()
      const params = new URLSearchParams(new frameWindow.FormData(form))
      target.search = params.toString()
      navigateBrowse(target.href)
    }, true)
  }

  function restoreShellWidth() {
    let saved = 0
    let legacy = 0
    try {
      saved = Number(window.localStorage.getItem(SHELL_WIDTH_KEY) || 0)
      legacy = Number(window.localStorage.getItem(LEGACY_SHELL_WIDTH_KEY) || 0)
    } catch (_) {}
    const width = preferredShellWidth(saved, legacy, window.innerWidth)
    if (width > 0) setShellWidth(width, false)
    else syncResizeHandle(Math.min(630, shellMaxWidth()))
    if (!saved && legacy && width > 0) {
      try { window.localStorage.setItem(SHELL_WIDTH_KEY, String(width)) } catch (_) {}
    }
    if (typeof window.ResizeObserver === 'undefined') return
    let lastWidth = 0
    new window.ResizeObserver(function () {
      if (!activeSurface) return
      const next = Math.round(shell.getBoundingClientRect().width || 0)
      if (next < 300 || next === lastWidth) return
      lastWidth = next
      syncResizeHandle(next)
      try { window.localStorage.setItem(SHELL_WIDTH_KEY, String(next)) } catch (_) {}
    }).observe(shell)
  }

  function shellMaxWidth() {
    return Math.min(SHELL_MAX_WIDTH, Math.max(0, window.innerWidth - 16))
  }

  function syncResizeHandle(width) {
    if (!resizeHandle) return
    resizeHandle.setAttribute('aria-valuemin', String(Math.min(SHELL_MIN_WIDTH, shellMaxWidth())))
    resizeHandle.setAttribute('aria-valuemax', String(shellMaxWidth()))
    resizeHandle.setAttribute('aria-valuenow', String(Math.round(width || 0)))
  }

  function setShellWidth(value, persist) {
    const max = shellMaxWidth()
    const min = Math.min(SHELL_MIN_WIDTH, max)
    const next = Math.round(Math.min(max, Math.max(min, Number(value) || min)))
    shell.style.width = next + 'px'
    syncResizeHandle(next)
    if (persist) {
      try { window.localStorage.setItem(SHELL_WIDTH_KEY, String(next)) } catch (_) {}
    }
    return next
  }

  function bindResizeHandle() {
    if (!resizeTargets.length) return
    let pointerID = null
    let pointerTarget = null
    let direction = 'left'
    let startX = 0
    let startY = 0
    let startWidth = 0
    let startHeight = 0
    let startRect = null

    resizeTargets.forEach(function (target) {
      target.addEventListener('pointerdown', function (event) {
        if (event.button !== 0) return
        event.preventDefault()
        pointerID = event.pointerId
        pointerTarget = target
        direction = target.getAttribute('data-mini-player-resize') || 'left'
        startX = event.clientX
        startY = event.clientY
        startRect = shell.getBoundingClientRect()
        startWidth = startRect.width
        startHeight = startRect.height
        shell.style.left = Math.round(startRect.left) + 'px'
        shell.style.top = Math.round(startRect.top) + 'px'
        shell.style.right = 'auto'
        shell.style.bottom = 'auto'
        target.setPointerCapture(event.pointerId)
        doc.documentElement.classList.add('mini-player-resizing')
      })
      target.addEventListener('pointermove', function (event) {
        if (event.pointerId !== pointerID || target !== pointerTarget || !startRect) return
        const width = resizedShellWidth(
          direction,
          startWidth,
          startHeight,
          event.clientX - startX,
          event.clientY - startY
        )
        setShellWidth(width, false)
        const nextRect = shell.getBoundingClientRect()
        const left = direction.includes('right') ? startRect.left : startRect.right - nextRect.width
        const top = direction.includes('bottom') ? startRect.top : startRect.bottom - nextRect.height
        shell.style.left = Math.round(left) + 'px'
        shell.style.top = Math.round(top) + 'px'
      })
      target.addEventListener('pointerup', finishResize)
      target.addEventListener('pointercancel', finishResize)
    })
    function finishResize(event) {
      if (event.pointerId !== pointerID || !pointerTarget) return
      setShellWidth(shell.getBoundingClientRect().width, true)
      const finalRect = shell.getBoundingClientRect()
      shell.style.left = ''
      shell.style.top = ''
      shell.style.right = Math.round(window.innerWidth - finalRect.right) + 'px'
      shell.style.bottom = Math.round(window.innerHeight - finalRect.bottom) + 'px'
      const target = pointerTarget
      pointerID = null
      pointerTarget = null
      startRect = null
      doc.documentElement.classList.remove('mini-player-resizing')
      if (target.hasPointerCapture(event.pointerId)) target.releasePointerCapture(event.pointerId)
    }
    if (!resizeHandle) return
    resizeHandle.addEventListener('keydown', function (event) {
      const current = shell.getBoundingClientRect().width
      let next = current
      if (event.key === 'Home') next = SHELL_MIN_WIDTH
      else if (event.key === 'End') next = shellMaxWidth()
      else if (event.key === 'ArrowLeft') next += 16
      else if (event.key === 'ArrowRight') next -= 16
      else return
      event.preventDefault()
      setShellWidth(next, true)
    })
  }

  function bindYouTubeButton(ownerDocument) {
    const surface = youtubeSurface(ownerDocument)
    if (!surface || surface.button.dataset.miniPlayerBound === '1') return
    surface.button.dataset.miniPlayerBound = '1'
    surface.video.addEventListener('play', function () { handleYouTubePlayback(surface.video) })
    surface.button.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      if (activeSurface && activeSurface.element === surface.element) returnToSurface()
      else dockSurface(surface)
    })
  }

  doc.addEventListener('click', function (event) {
    if (browseActive) return
    const anchor = linkForClick(event, doc)
    if (!anchor || !isEligibleURL(anchor.href)) return

    if (activeSurface && sameSurfaceHome(anchor.href)) {
      event.preventDefault()
      returnToSurface()
      return
    }

    if (activeSurface && activeSurface.kind === 'videos' && isPlayerURL(anchor.href, window.location.href)) {
      event.preventDefault()
      leaveMiniPlayerForPlayer(anchor.href)
      return
    }

    if (activeSurface) {
      event.preventDefault()
      navigateBrowse(anchor.href)
      return
    }

    const candidate = playingSurface(doc)
    if (!candidate || !shouldAutomaticallyMini(candidate.kind, preferences())) return
    event.preventDefault()
    if (dockSurface(candidate)) navigateBrowse(anchor.href)
  })

  if (returnButton) returnButton.addEventListener('click', returnToSurface)
  if (closeButton) closeButton.addEventListener('click', closeMiniPlayer)
  function bindBrowseFrame(frame) {
    frame.addEventListener('load', function () {
      if (frame === browseFrame) installFrameNavigation()
    })
  }

  bindBrowseFrame(browseFrame)
  window.addEventListener('popstate', function () {
    if (!browseActive) return
    if (activeSurface && sameSurfaceHome(window.location.href)) returnToSurface({ push: false })
    else navigateBrowse(window.location.href, { push: false })
  })

  bindYouTubeButton(doc)
  window.history.replaceState(Object.assign({}, window.history.state || {}, {
    iglooMiniSurfaceHome: window.location.pathname + window.location.search,
  }), '', window.location.href)
  bindResizeHandle()
  restoreShellWidth()

  window.IglooMiniPlayer = {
    isMini: function () { return !!activeSurface },
    ownsSurface: function (element) { return !!activeSurface && activeSurface.element === element },
    fullscreenTarget: function (fallback) { return activeSurface ? activeSurface.element : fallback },
    fullscreenTargetFor: function (element, fallback) {
      return activeSurface && activeSurface.element === element ? activeSurface.element : fallback
    },
    returnToVideo: returnToSurface,
    enterManual: function () {
      const surface = youtubeSurface(doc)
      if (surface) dockSurface(surface)
    },
    enterSurface: function (surface) { return dockSurface(surface) },
    youtubePlaybackStarted: handleYouTubePlayback,
    toggleSurface: function (surface) {
      if (activeSurface && surface && activeSurface.element === surface.element) returnToSurface()
      else dockSurface(surface)
    },
  }
}

function initFramePlayerBridge() {
  const doc = document
  const root = doc.getElementById('player-root')
  const wrapper = root && root.querySelector('.player-wrapper')
  const video = doc.getElementById('mpa-player')
  const button = doc.getElementById('player-mini-btn')
  if (!root || !wrapper || !video || !button) return
  if ((root.getAttribute('data-channel-platform') || '').toLowerCase() !== 'youtube') return
  const titleElement = doc.getElementById('player-title')

  function manager() {
    try { return window.top && window.top.IglooMiniPlayer } catch (_) { return null }
  }

  const surface = {
    element: wrapper,
    video: video,
    button: button,
    title: String(titleElement && titleElement.textContent || '').trim() || doc.title,
    kind: 'videos',
    sourceDocument: doc,
  }
  button.addEventListener('click', function (event) {
    event.preventDefault()
    event.stopPropagation()
    const owner = manager()
    if (owner && typeof owner.toggleSurface === 'function') owner.toggleSurface(surface)
  })
  video.addEventListener('play', function () {
    const owner = manager()
    if (owner && typeof owner.youtubePlaybackStarted === 'function') owner.youtubePlaybackStarted(video)
  })
  if (!video.paused) {
    const owner = manager()
    if (owner && typeof owner.youtubePlaybackStarted === 'function') owner.youtubePlaybackStarted(video)
  }

  window.IglooMiniPlayer = {
    isMini: function () {
      const owner = manager()
      return !!(owner && owner.ownsSurface && owner.ownsSurface(wrapper))
    },
    fullscreenTarget: function (fallback) {
      const owner = manager()
      return owner && owner.fullscreenTargetFor ? owner.fullscreenTargetFor(wrapper, fallback) : fallback
    },
    enterManual: function () {
      const owner = manager()
      if (owner && owner.enterSurface) owner.enterSurface(surface)
    },
  }
}

if (typeof window !== 'undefined' && typeof document !== 'undefined') initMiniPlayer()
