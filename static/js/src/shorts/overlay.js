// Shorts overlay — visibility, navigation, virtual windowing, scroll.

import { pauseAllShorts, disposeShortItem } from './playback.js'
import { setSlideshowIndex, startSlideshowPlayback } from './playback.js'
import { materialIconMarkup, t, tf } from '../utils.js'
import { recordShortsDebugEvent } from './debug.js'

var _state = null
var _dom = null
var _fns = null
var _deckTransitionTimer = 0
var DECK_WINDOW = 10
var DECK_TRANSITION_MS = 340
var _endDistance = 0
var _endTimer = 0
var _endRequest = null
var _endPulling = false

// initOverlay sets up module-level refs.
//   dom: { shortsContainer, gridShell, layout, upToDateOverlay, sourceContainer,
//          sourceCardSelector, doc }
//   fns: { closeBookmarkMenu, ensureGridThumbnails, updateTopControls,
//          updateCurrentActionButtons, setLastViewedShortId, setLastViewedShortResume,
//          beginOpenRequest, getShortsInfiniteController, makeShortItem, parseCardData, iconSvg }
export function initOverlay(stateRef, dom, fns) {
  _state = stateRef
  _dom = dom
  _fns = fns
}

function setSvgContent(el, svgString) {
  el.replaceChildren()
  var tmp = document.createElement('template')
  tmp.innerHTML = svgString // nosec: static SVG from iconSvg — no user input
  el.appendChild(tmp.content)
}

function qa(sel, root) {
  return Array.prototype.slice.call((root || _dom.doc).querySelectorAll(sel))
}

function currentData() {
  if (_state.currentIndex < 0 || _state.currentIndex >= _state.items.length) return null
  return _state.items[_state.currentIndex] ? _state.items[_state.currentIndex].data : null
}

function isSkeletonCard(card) {
  if (_state && typeof _state.isSkeletonCard === 'function') return _state.isSkeletonCard(card)
  return !!(card && card.getAttribute && card.getAttribute('data-shorts-card-skeleton') === '1')
}

export function cancelShortsNavigationIntent() {
  if (!_state) return
  clearDeckTransition()
  resetMomentsEnd()
}

function deckState() {
  if (!_state.deck) _state.deck = { phase: 'idle', targetIndex: -1, fromIndex: -1 }
  return _state.deck
}

function deckIsTransitioning() {
  return !_state.storyMode && deckState().phase === 'transitioning'
}

function clearDeckTransition() {
  if (_deckTransitionTimer) {
    try { clearTimeout(_deckTransitionTimer) } catch (_) {}
    _deckTransitionTimer = 0
  }
  if (_state && _state.deck) {
    _state.deck.phase = 'idle'
    _state.deck.targetIndex = -1
    _state.deck.fromIndex = -1
  }
}

function forceDeckLayout() {
  try { void _dom.shortsContainer.offsetHeight } catch (_) {}
}

function setDeckItemPosition(entry, index, centerIndex, animate) {
  if (!entry || !entry.el) return
  var offset = (index - centerIndex) * 100
  entry.el.style.transition = animate ? ('transform ' + DECK_TRANSITION_MS + 'ms cubic-bezier(0.22, 0.61, 0.36, 1)') : 'none'
  entry.el.style.transform = _endDistance > 0 && index === centerIndex
    ? 'translate3d(0, calc(' + offset + '% - ' + _endDistance + 'px), 0)'
    : 'translate3d(0, ' + offset + '%, 0)'
  entry.el.style.visibility = Math.abs(index - centerIndex) <= 2 ? '' : 'hidden'
}

function positionDeckItems(centerIndex, animate) {
  if (_state.storyMode || !Number.isFinite(centerIndex)) return
  _state.items.forEach(function (entry, index) {
    if (!entry) return
    setDeckItemPosition(entry, index, centerIndex, !!animate)
  })
}

function resetSlideshowFromStart(entry) {
  var slideshow = entry && entry.refs && entry.refs.slideshow
  if (!slideshow) return
  if (slideshow.timer) {
    try { clearTimeout(slideshow.timer) } catch (_) {}
    slideshow.timer = 0
  }
  if (slideshow.audio) {
    try {
      slideshow.audio.pause()
      slideshow.audio.currentTime = 0
    } catch (_) {}
  }
  setSlideshowIndex(entry, 0)
}

function playEntryFromStart(entry) {
  var video = entry && entry.refs && entry.refs.video
  if (video) {
    playShortVideoFromStart(entry)
    return
  }
  if (entry && entry.refs && entry.refs.slideshow) {
    resetSlideshowFromStart(entry)
    startSlideshowPlayback(entry)
  }
}

function ensureEntryAtIndex(index) {
  if (!Number.isFinite(index) || index < 0 || index >= _state.cards.length) return null
  if (_state.items[index]) return _state.items[index]
  var card = _state.cards[index]
  if (!card || isSkeletonCard(card)) return null
  var data = _fns.parseCardData(card)
  if (!data) return null
  var entry = _fns.makeShortItem(data)
  _state.items[index] = entry
  _state.byId.set(data.id, entry)
  _dom.shortsContainer.appendChild(entry.el)
  if (_state.storyMode && _state.observer) _state.observer.observe(entry.el)
  return entry
}

function recomputeRenderedBounds() {
  var start = -1
  var end = -1
  _state.items.forEach(function (entry, index) {
    if (!entry) return
    if (start < 0) start = index
    end = index
  })
  _state.renderedStart = start
  _state.renderedEnd = end
}

function pruneDeckWindow(centerIndex) {
  if (_state.storyMode || !Number.isFinite(centerIndex)) return
  var start = Math.max(0, centerIndex - DECK_WINDOW)
  var end = Math.min(_state.cards.length - 1, centerIndex + DECK_WINDOW)
  _state.items.forEach(function (entry, index) {
    if (!entry || (index >= start && index <= end)) return
    disposeShortItem(entry)
    if (entry.el && entry.el.parentNode) entry.el.parentNode.removeChild(entry.el)
    if (entry.data && entry.data.id) _state.byId.delete(entry.data.id)
    _state.items[index] = null
  })
  recomputeRenderedBounds()
}

function ensureDeckRange(centerIndex, opts) {
  if (!Number.isFinite(centerIndex)) return
  var start = Math.max(0, centerIndex - DECK_WINDOW)
  var end = Math.min(_state.cards.length - 1, centerIndex + DECK_WINDOW)
  for (var i = start; i <= end; i += 1) ensureEntryAtIndex(i)
  recomputeRenderedBounds()
  if (!(opts && opts.skipPosition)) positionDeckItems(centerIndex, false)
  warmNearbyShortVideos(centerIndex)
}

function hydrateCardAtIndex(index, opts) {
  if (!Number.isFinite(index) || index < 0 || index >= _state.cards.length) return false
  var card = _state.cards[index]
  if (!isSkeletonCard(card) || typeof _state.hydrateCardElement !== 'function') return false
  var videoId = String(card.getAttribute('data-video-id') || '').trim()
  _state.hydrateCardElement(card).then(function () {
    if (opts && (opts.open || opts.navigate) && Number(_state.openRequestSeq || 0) !== Number(opts.openRequestSeq || 0)) return
    if (opts && (opts.open || opts.navigate) && !String(opts.openVideoId || '').trim()) return
    appendNewItemsFromGrid()
    var hydratedIndex = _state.cardIndexById.get(videoId)
    if (hydratedIndex === undefined) return
    var hydrated = _state.cards[hydratedIndex]
    if (!hydrated || isSkeletonCard(hydrated)) return
    if (opts && opts.open) {
      if (String(hydrated.getAttribute('data-video-id') || '').trim() !== String(opts.openVideoId || '').trim()) return
      openOverlayAtIndex(hydratedIndex, opts.immediate, {
        persistCursor: opts.persistCursor !== false
      })
      return
    }
    if (!_state.overlayOpen) return
    if (!_state.storyMode) {
      ensureEntryAtIndex(hydratedIndex)
      ensureDeckRange(_state.currentIndex >= 0 ? _state.currentIndex : hydratedIndex)
      if (opts && opts.navigate) {
        scrollToIndex(hydratedIndex, (opts && opts.behavior) || 'smooth')
      }
      return
    }
    if (!_state.items[index] && index >= _state.renderedStart && index <= _state.renderedEnd) {
      renderShortsWindow(index)
    } else if (!_state.items[index]) {
      if (index < _state.renderedStart) {
        _extendBackwardTo(index)
      } else if (index > _state.renderedEnd) {
        _extendForwardTo(index)
      }
    }
    if (!(opts && opts.preloadOnly)) {
      scrollToIndex(index, (opts && opts.behavior) || 'smooth')
      activateIndex(index, { force: true, play: !(opts && opts.play === false) })
    }
  }).catch(function () {})
  return true
}

function retryAutoplayMuted(video) {
  if (!video || _state.muted) return
  _state.muted = true
  try { localStorage.setItem('shortsMuted', 'true') } catch (_) {}
  _state.items.forEach(function (entry) {
    var v = entry && entry.refs && entry.refs.video
    if (v) v.muted = true
    var a = entry && entry.refs && entry.refs.slideshow && entry.refs.slideshow.audio
    if (a) a.muted = true
  })
  updateCurrentActionButtons()
  try {
    var retry = video.play()
    _state.activePlayPromise = retry || null
    if (retry && typeof retry.catch === 'function') retry.catch(function () {})
  } catch (_) {}
}

function handleAutoplayRejected(video, err) {
  var name = String(err && err.name || '')
  var message = String(err && err.message || '')
  if (name === 'NotAllowedError' || /user gesture|not allowed|permission/i.test(message)) {
    retryAutoplayMuted(video)
  }
}

function revealShortVideoIfReady(entry, video) {
  if (!entry || !video || !entry.refs || !entry.refs.wrapper) return
  if (video.readyState >= 2) entry.refs.wrapper.classList.remove('is-awaiting-first-frame')
}

function playShortVideo(entry, video) {
  if (!entry || !video) return
  var manager = null
  try { manager = window.top && window.top.IglooMiniPlayer } catch (_) {}
  var isMini = manager && typeof manager.isMini === 'function' && manager.isMini() &&
    typeof manager.currentKind === 'function' && manager.currentKind() === 'moments'
  if (!_state.overlayOpen && !isMini) return
  var current = currentData()
  if (!current || current.id !== entry.data.id) return
  recordShortsDebugEvent(entry, 'play:attempt')
  revealShortVideoIfReady(entry, video)
  try {
    var promise = entry.refs.playLive ? entry.refs.playLive() : video.play()
    if (entry.refs.playLive && promise) {
      promise.then(function () {
        var active = currentData()
        if (active && active.id === entry.data.id) updateUrlForCurrent()
      }).catch(function () {})
    }
    _state.activePlayPromise = promise || null
    if (promise && typeof promise.catch === 'function') {
      promise.catch(function (err) { handleAutoplayRejected(video, err) })
    }
  } catch (err) {
    handleAutoplayRejected(video, err)
  }
}

function playShortVideoFromStart(entry) {
  var video = entry && entry.refs && entry.refs.video
  if (!video) return
  try {
    if (entry.refs.playLive) { playShortVideo(entry, video); return }
    video.currentTime = 0
  } catch (_) {}
  playShortVideo(entry, video)
}

function warmShortVideo(entry, active) {
  var video = entry && entry.refs && entry.refs.video
  if (!video) return
  if (entry.refs.playLive) return
  try {
    if (!active) {
      video.preload = 'none'
      if (!video._shortsPrewarmStarted || !video.paused || typeof video.load !== 'function') return
      video._shortsPrewarmStarted = false
      video.load()
      return
    }
    video.preload = 'auto'
    if (video._shortsPrewarmStarted || video.readyState >= 3) return
    var atStart = Math.abs(Number(video.currentTime || 0)) < 0.01
    if (!atStart || typeof video.load !== 'function') return
    video._shortsPrewarmStarted = true
    video.load()
  } catch (_) {}
}

function warmNearbyShortVideos(index) {
  if (!Number.isFinite(index) || !_state || !_state.items) return
  var previous = Array.isArray(_state.warmVideoIndexes) ? _state.warmVideoIndexes : []
  var next = []
  for (var offset = -1; offset <= 1; offset += 1) {
    var wanted = index + offset
    if (wanted >= 0 && wanted < _state.items.length) next.push(wanted)
  }
  var candidates = new Set(previous.concat(next))
  candidates.forEach(function (i) {
    var entry = _state.items[i]
    if (!entry) return
    warmShortVideo(entry, next.indexOf(i) >= 0)
  })
  _state.warmVideoIndexes = next
}

function releaseWarmShortVideos() {
  if (!_state || !_state.items) return
  var previous = Array.isArray(_state.warmVideoIndexes) ? _state.warmVideoIndexes : []
  for (var i = 0; i < previous.length; i += 1) {
    warmShortVideo(_state.items[previous[i]], false)
  }
  _state.warmVideoIndexes = []
}

export function setOverlayVisible(visible) {
  _state.overlayOpen = !!visible
  if (_dom.gridShell) _dom.gridShell.classList.toggle('hidden', _state.overlayOpen)
  _dom.layout.classList.toggle('hidden', !_state.overlayOpen)
  _dom.doc.body.classList.toggle('shorts-mode', _state.overlayOpen)
  _dom.doc.body.classList.toggle('shorts-open', _state.overlayOpen)
  if (!_state.overlayOpen) {
    clearDeckTransition()
    resetMomentsEnd()
    pauseAllShorts()
    releaseWarmShortVideos()
    _fns.closeBookmarkMenu()
    var card = _state.cards[_state.currentIndex]
    if (card && typeof card.scrollIntoView === 'function') {
      requestAnimationFrame(function () {
        setTimeout(function () {
          try { card.scrollIntoView({ behavior: 'auto', block: 'center' }) } catch (_) { card.scrollIntoView() }
        }, 50)
      })
    }
    _fns.ensureGridThumbnails()
  } else {
    if (_state.storyMode) ensureObserver()
    else positionDeckItems(_state.currentIndex, false)
    if (_dom.shortsContainer && typeof _dom.shortsContainer.focus === 'function') {
      _dom.shortsContainer.focus({ preventScroll: true })
    }
  }
}

export function updateUrlForCurrent() {
  return
}

function setEndFeedback(key, fallback) {
  if (!_dom.upToDateOverlay) return
  var label = _dom.upToDateOverlay.querySelector('span')
  var text = t(key, fallback)
  if (label.textContent !== text) label.textContent = text
  _dom.upToDateOverlay.classList.remove('hidden')
}

function setEndDistance(distance, animate) {
  clearTimeout(_endTimer)
  _endDistance = distance
  positionDeckItems(_state.currentIndex, animate)
  if (!_dom.upToDateOverlay) return
  _dom.upToDateOverlay.style.transition = animate ? 'height ' + DECK_TRANSITION_MS + 'ms ease-out' : 'none'
  _dom.upToDateOverlay.style.height = distance + 'px'
  if (distance === 0) {
    _endTimer = setTimeout(function () {
      _dom.upToDateOverlay.classList.add('hidden')
    }, animate ? DECK_TRANSITION_MS : 0)
  }
}

function resetMomentsEnd() {
  clearTimeout(_endTimer)
  _endTimer = 0
  _endRequest = null
  _endPulling = false
  _endDistance = 0
  if (!_dom.upToDateOverlay) return
  _dom.upToDateOverlay.style.height = '0px'
  _dom.upToDateOverlay.classList.add('hidden')
}

function returnFromMomentsEnd() {
  clearTimeout(_endTimer)
  if (_endPulling) return
  _endTimer = setTimeout(function () { setEndDistance(0, true) }, 800)
}

export function canPullMomentsEnd() {
  return !_state.storyMode && !deckIsTransitioning() && _state.currentIndex >= 0 && _state.currentIndex + 1 >= _state.cards.length
}

export function pullMomentsEnd(distance, wheel) {
  if (!canPullMomentsEnd()) return false
  clearTimeout(_endTimer)
  _endPulling = true
  var limit = _dom.shortsContainer.offsetHeight * 0.5
  var pull = Math.max(0, distance)
  setEndDistance(limit * pull / (limit + pull), false)
  if (!wheel) setEndFeedback('status_loading', 'Loading...')
  return true
}

export function releaseMomentsEndPull() {
  _endPulling = false
  if (!_endDistance) return
  setEndDistance(Math.min(80, _endDistance), true)
  if (!_endRequest) returnFromMomentsEnd()
}

export function cancelMomentsEndPull() {
  clearTimeout(_endTimer)
  _endRequest = null
  _endPulling = false
  if (!_endDistance) {
    if (_dom.upToDateOverlay) _dom.upToDateOverlay.classList.add('hidden')
    return
  }
  setEndDistance(0, true)
}

function scrollStoryToIndex(index, behavior) {
  if (hydrateCardAtIndex(index, { behavior: behavior || 'smooth', play: true })) return true
  if (!_state.items[index]) {
    if (index < _state.renderedStart && _state.renderedStart > 0) {
      _extendBackwardTo(Math.max(0, index - 5))
    } else if (index > _state.renderedEnd && _state.renderedEnd < _state.cards.length - 1) {
      _extendForwardTo(Math.min(_state.cards.length - 1, index + 5))
    }
  }
  var entry = _state.items[index]
  if (!entry || !entry.el) return false
  warmNearbyShortVideos(index)
  try {
    if (behavior === 'smooth') {
      entry.el.scrollIntoView({ inline: 'start', block: 'nearest', behavior: 'smooth' })
    } else {
      var previousScrollBehavior = _dom.shortsContainer.style.scrollBehavior
      _dom.shortsContainer.style.scrollBehavior = 'auto'
      var left = entry.el.offsetLeft || 0
      if (typeof _dom.shortsContainer.scrollTo === 'function') {
        _dom.shortsContainer.scrollTo({ left: left, top: 0, behavior: 'auto' })
      } else {
        _dom.shortsContainer.scrollLeft = left
      }
      requestAnimationFrame(function () {
        _dom.shortsContainer.style.scrollBehavior = previousScrollBehavior
      })
    }
  } catch (_) {
    entry.el.scrollIntoView()
  }
  activateIndex(index, { force: true, play: true })
  return true
}

function completeDeckTransition() {
  var deck = deckState()
  if (deck.phase !== 'transitioning') return
  var index = deck.targetIndex
  clearDeckTransition()
  positionDeckItems(index, false)
  var entry = _state.items[index]
  persistShortPosition(index, entry)
  pruneDeckWindow(index)
  ensureDeckRange(index)
  updateCurrentActionButtons()
}

function startDeckTransition(index) {
  if (deckIsTransitioning()) return true
  var target = ensureEntryAtIndex(index)
  if (!target) {
    hydrateCardAtIndex(index, { preloadOnly: true })
    warmNearbyShortVideos(_state.currentIndex)
    return true
  }
  var fromIndex = _state.currentIndex
  if (fromIndex < 0 || fromIndex === index) {
    activateIndex(index, { force: true, play: true, persist: true })
    return true
  }
  ensureDeckRange(index, { skipPosition: true })
  positionDeckItems(fromIndex, false)
  forceDeckLayout()
  var deck = deckState()
  deck.phase = 'transitioning'
  deck.fromIndex = fromIndex
  deck.targetIndex = index
  activateIndex(index, { force: true, play: true, persist: false, updatePosition: false })
  recordShortsDebugEvent(target, 'deck:transition-start', { fromIndex: fromIndex, targetIndex: index })
  requestAnimationFrame(function () {
    if (deckState().phase !== 'transitioning' || deckState().targetIndex !== index) return
    positionDeckItems(index, true)
  })
  _deckTransitionTimer = setTimeout(completeDeckTransition, DECK_TRANSITION_MS)
  return true
}

export function scrollToIndex(index, behavior, options) {
  if (!Number.isFinite(index)) return false
  if (index < 0 || index >= _state.cards.length) return false
  if (_endDistance || _endRequest) {
    resetMomentsEnd()
    positionDeckItems(_state.currentIndex, false)
  }
  if (_state.storyMode) return scrollStoryToIndex(index, behavior)
  if (deckIsTransitioning()) return true
  if (isSkeletonCard(_state.cards[index])) {
    hydrateCardAtIndex(index, {
      preloadOnly: !(options && options.navigate),
      navigate: !!(options && options.navigate),
      behavior: behavior,
      openRequestSeq: Number(_state.openRequestSeq || 0),
      openVideoId: String(_state.cards[index].getAttribute('data-video-id') || '').trim()
    })
    return true
  }
  var entry = ensureEntryAtIndex(index)
  if (!entry) return false
  if (behavior === 'instant') {
    clearDeckTransition()
    activateIndex(index, { force: true, play: true, persist: true })
    pruneDeckWindow(index)
    ensureDeckRange(index)
    return true
  }
  return startDeckTransition(index)
}

export function requestMoreIfNeeded() {
  if (_state.storyMode) return
  var remaining = _state.cards.length - (_state.currentIndex + 1)
  if (remaining <= 4) {
    var ctl = _fns.getShortsInfiniteController()
    if (ctl && !ctl.loading && !ctl.failed && ctl.nextUrl()) {
      ctl.loadNext()
    } else if (window.MpaInfinitePage && typeof window.MpaInfinitePage.refreshAll === 'function') {
      window.MpaInfinitePage.refreshAll()
    }
  }
}

export function goNext(options) {
  if (deckIsTransitioning()) return
  _endPulling = false
  if (options && options.explicit && _fns && typeof _fns.beginOpenRequest === 'function') _fns.beginOpenRequest()
  if (scrollToIndex(_state.currentIndex + 1, _state.storyMode ? 'instant' : 'smooth', { navigate: true })) return
  if (_state.storyMode) {
    if (_fns && typeof _fns.handleStoryEnd === 'function' && _fns.handleStoryEnd()) return
    showGrid()
    return
  }
  requestMoreIfNeeded()
  clearTimeout(_endTimer)
  setEndDistance(Math.min(80, _dom.shortsContainer.offsetHeight * 0.25), true)
  if (_endRequest) {
    _endRequest.sequence = Number(_state.openRequestSeq || 0)
    return
  }
  setEndFeedback('status_loading', 'Loading...')
  if (_fns && typeof _fns.refreshMomentsSession === 'function') {
    var request = { sequence: Number(_state.openRequestSeq || 0) }
    _endRequest = request
    _fns.refreshMomentsSession().then(function () {
      if (_endRequest !== request) return
      _endRequest = null
      if (!_state.overlayOpen || Number(_state.openRequestSeq || 0) !== request.sequence) { cancelMomentsEndPull(); return }
      if (_state.currentIndex + 1 < _state.cards.length) { goNext(); return }
      setEndFeedback('status_up_to_date', "You're up to date!")
      returnFromMomentsEnd()
    }).catch(function () {
      if (_endRequest !== request) return
      _endRequest = null
      if (!_state.overlayOpen || Number(_state.openRequestSeq || 0) !== request.sequence) { cancelMomentsEndPull(); return }
      setEndFeedback('error_refresh_failed', 'Refresh failed')
      returnFromMomentsEnd()
    })
    return
  }
  setEndFeedback('status_up_to_date', "You're up to date!")
  returnFromMomentsEnd()
}

export function goPrev(options) {
  if (options && options.explicit && _fns && typeof _fns.beginOpenRequest === 'function') _fns.beginOpenRequest()
  scrollToIndex(_state.currentIndex - 1, 'smooth', { navigate: true })
}

export function ensureCurrentVisible(index, immediate) {
  if (!Number.isFinite(index)) return
  scrollToIndex(index, immediate ? 'instant' : 'smooth')
}

export function updateCurrentActionButtons() {
  var d = currentData()
  _state.items.forEach(function (entry, idx) {
    if (!entry || !entry.refs) return
    var refs = entry.refs
    var isCurrent = idx === _state.currentIndex
    if (refs.muteBtn) {
      refs.muteBtn.classList.toggle('active', _state.muted)
      setSvgContent(refs.muteBtn, _fns.iconSvg('mute', _state.muted, _state.volume))
      refs.muteBtn.title = _state.muted ? t('action_unmute', 'Unmute') : t('action_mute', 'Mute')
      refs.muteBtn.setAttribute('aria-label', refs.muteBtn.title)
    }
    if (refs.volumeSlider) refs.volumeSlider.value = String(_state.muted ? 0 : _state.volume)
    if (refs.autoplayBtn) {
      var autoAdvance = _state.storyMode || _state.autoPlayNext
      refs.autoplayBtn.classList.toggle('active', autoAdvance && isCurrent)
      setSvgContent(refs.autoplayBtn, _fns.iconSvg('autoplay', autoAdvance && isCurrent))
      refs.autoplayBtn.title = tf('shorts_autoplay_next_state', 'Auto-play next short: %1$s', autoAdvance ? t('state_on', 'ON') : t('state_off', 'OFF'))
    }
    if (refs.miniControls) {
      var miniAutoplay = refs.miniControls.querySelector('[data-feed-video-autoplay]')
      if (miniAutoplay) {
        var autoAdvanceMini = _state.storyMode || _state.autoPlayNext
        miniAutoplay.classList.toggle('active', autoAdvanceMini)
        miniAutoplay.setAttribute('aria-pressed', autoAdvanceMini ? 'true' : 'false')
        var miniLabel = tf('shorts_autoplay_next_state', 'Auto-play next short: %1', autoAdvanceMini ? t('state_on', 'ON') : t('state_off', 'OFF'))
        miniAutoplay.setAttribute('title', miniLabel)
        miniAutoplay.setAttribute('aria-label', miniLabel)
        setSvgContent(miniAutoplay, materialIconMarkup(autoAdvanceMini ? 'PlayCircle' : 'PlayCircleOutline'))
      }
    }
    if (refs.bookmarkBtn) {
      refs.bookmarkBtn.classList.toggle('active', !!entry.data.bookmarked)
      setSvgContent(refs.bookmarkBtn, _fns.iconSvg('bookmark', !!entry.data.bookmarked))
      refs.bookmarkBtn.title = entry.data.bookmarked ? t('action_bookmarked', 'Bookmarked') : t('action_bookmark', 'Bookmark')
    }
    if (refs.commentBtn) refs.commentBtn.classList.toggle('active', false)
  })
  if (d) _fns.updateTopControls()
}

function persistShortPosition(index, entry) {
  if (!entry || !_state || _state.currentIndex !== index) return
  if (_state.items[index] !== entry) return
  if (entry.data.liveStatus === 'is_live' && !entry.refs.liveRegistered) {
    entry.refs.onLiveRegistered = function () { persistShortPosition(index, entry) }
    return
  }
  if (typeof _fns.markShortViewed === 'function') _fns.markShortViewed(entry.data.id)
  _fns.setLastViewedShortId(entry.data.id)
  _fns.setLastViewedShortResume(entry.data.id, index, entry.data.page, entry.data.sortAtMs)
}

function recordShortView(index, entry) {
  if (!entry || !_state || _state.currentIndex !== index) return
  if (_state.items[index] !== entry) return
  if (entry.data.liveStatus === 'is_live' && !entry.refs.liveRegistered) {
    entry.refs.onLiveRegistered = function () { recordShortView(index, entry) }
    return
  }
  if (typeof _fns.markShortViewed === 'function') _fns.markShortViewed(entry.data.id)
}

function activateVisibleShort(index) {
  if (_state.storyMode) {
    activateIndex(index, { force: false })
  }
}

export function activateIndex(index, options) {
  var opts = options || {}
  if (!Number.isFinite(index) || index < 0 || index >= _state.cards.length) return
  if (_state.currentIndex === index && !opts.force) {
    _fns.updateTopControls()
    return
  }
  var prevEntry = _state.currentIndex >= 0 && _state.items[_state.currentIndex]
  if (prevEntry && prevEntry.el) {
    prevEntry.el.classList.remove('is-active')
  }
  _state.currentIndex = index
  var entry = _state.items[index] || ensureEntryAtIndex(index)
  if (!entry || !entry.refs) return
  entry.el.classList.add('is-active')
  var shouldPersist = opts.persist
  if (shouldPersist === undefined) shouldPersist = true
  if (_state.storyMode) {
    extendShortsWindow()
  } else {
    ensureDeckRange(index, { skipPosition: opts.updatePosition === false })
    if (opts.updatePosition !== false) positionDeckItems(index, !!opts.animate)
  }
  recordShortsDebugEvent(entry, 'activate', { deck: !_state.storyMode })
  recordShortsDebugEvent(entry, 'chrome:snapshot', { phase: 'activate', deck: !_state.storyMode })

  pauseAllShorts(entry.data.id)
  _state.lastVisibleId = entry.data.id
  if (shouldPersist) persistShortPosition(index, entry)
  else if (opts.recordView) recordShortView(index, entry)
  updateUrlForCurrent()
  requestMoreIfNeeded()
  updateCurrentActionButtons()

  var manager = null
  try { manager = window.top && window.top.IglooMiniPlayer } catch (_) {}
  if (manager && typeof manager.isMini === 'function' && manager.isMini() &&
      typeof manager.currentKind === 'function' && manager.currentKind() === 'moments') {
    if (_fns && typeof _fns.dockMomentMiniPlayer === 'function') {
      _fns.dockMomentMiniPlayer(entry)
    }
  }

  warmNearbyShortVideos(index)
  if (opts.play !== false) playEntryFromStart(entry)
  else if (entry.refs.slideshow) setSlideshowIndex(entry, entry.refs.slideshow.index || 0)
}

export function onShortIntersect(entries) {
  if (!_state.overlayOpen || !_state.storyMode) return
  var best = null
  entries.forEach(function (entry) {
    if (!entry.isIntersecting) return
    if (!best || entry.intersectionRatio > best.intersectionRatio) best = entry
  })
  if (!best) return
  var id = String(best.target.getAttribute('data-video-id') || '')
  if (!id) return
  var index = _state.cardIndexById.get(id)
  if (index !== undefined) {
    var entry = _state.items[index]
    if (entry) {
      recordShortsDebugEvent(entry, 'intersect:candidate', {
        ratio: Number((best.intersectionRatio || 0).toFixed(3)),
        story: true
      })
    }
    activateVisibleShort(index)
  }
}

export function ensureObserver() {
  if (!_state.storyMode) return
  if (_state.observer) return
  _state.observer = new IntersectionObserver(onShortIntersect, {
    root: _dom.shortsContainer,
    threshold: [0.6, 0.75, 0.9]
  })
  for (var i = _state.renderedStart; i <= _state.renderedEnd; i++) {
    var entry = _state.items[i]
    if (entry && entry.el) _state.observer.observe(entry.el)
  }
}

export function renderShortsWindow(centerIndex) {
  clearDeckTransition()
  if (_state.observer) {
    _state.observer.disconnect()
    _state.observer = null
  }
  _state.items.forEach(disposeShortItem)
  while (_dom.shortsContainer.firstChild) _dom.shortsContainer.removeChild(_dom.shortsContainer.firstChild)
  _state.items = new Array(_state.cards.length).fill(null)
  _state.byId = new Map()

  var start = Math.max(0, centerIndex - DECK_WINDOW)
  var end = Math.min(_state.cards.length - 1, centerIndex + DECK_WINDOW)

  var frag = _dom.doc.createDocumentFragment()
  for (var i = start; i <= end; i++) {
    var data = _fns.parseCardData(_state.cards[i])
    if (!data) continue
    var entry = _fns.makeShortItem(data)
    _state.items[i] = entry
    _state.byId.set(data.id, entry)
    frag.appendChild(entry.el)
  }
  _dom.shortsContainer.appendChild(frag)

  _state.renderedStart = start
  _state.renderedEnd = end
  if (!_state.storyMode) positionDeckItems(centerIndex, false)
  warmNearbyShortVideos(centerIndex)
  if (_state.storyMode) ensureObserver()
}

function _extendForwardTo(targetEnd) {
  var newEnd = Math.min(_state.cards.length - 1, targetEnd)
  if (newEnd <= _state.renderedEnd) return
  var frag = _dom.doc.createDocumentFragment()
  for (var i = _state.renderedEnd + 1; i <= newEnd; i++) {
    var data = _fns.parseCardData(_state.cards[i])
    if (!data) continue
    var entry = _fns.makeShortItem(data)
    _state.items[i] = entry
    _state.byId.set(data.id, entry)
    if (_state.storyMode && _state.observer) _state.observer.observe(entry.el)
    frag.appendChild(entry.el)
  }
  _dom.shortsContainer.appendChild(frag)
  _state.renderedEnd = newEnd
  if (!_state.storyMode) positionDeckItems(_state.currentIndex, false)
  warmNearbyShortVideos(_state.currentIndex)
}

function _extendBackwardTo(targetStart) {
  var newStart = Math.max(0, targetStart)
  if (newStart >= _state.renderedStart) return
  var scrollBefore = _state.storyMode ? _dom.shortsContainer.scrollTop : 0
  var heightBefore = _state.storyMode ? _dom.shortsContainer.scrollHeight : 0
  var frag = _dom.doc.createDocumentFragment()
  for (var i = newStart; i < _state.renderedStart; i++) {
    var data = _fns.parseCardData(_state.cards[i])
    if (!data) continue
    var entry = _fns.makeShortItem(data)
    _state.items[i] = entry
    _state.byId.set(data.id, entry)
    if (_state.storyMode && _state.observer) _state.observer.observe(entry.el)
    frag.appendChild(entry.el)
  }
  _dom.shortsContainer.insertBefore(frag, _dom.shortsContainer.firstChild)
  if (_state.storyMode) _dom.shortsContainer.scrollTop = scrollBefore + (_dom.shortsContainer.scrollHeight - heightBefore)
  _state.renderedStart = newStart
  if (!_state.storyMode) positionDeckItems(_state.currentIndex, false)
  warmNearbyShortVideos(_state.currentIndex)
}

export function extendShortsWindow() {
  if (_state.renderedEnd < 0 || _state.currentIndex < 0) return
  if (!_state.storyMode) {
    ensureDeckRange(_state.currentIndex)
    return
  }
  var remainingAhead = _state.renderedEnd - _state.currentIndex
  if (remainingAhead <= 5) {
    _extendForwardTo(_state.renderedEnd + 15)
  }
  var remainingBehind = _state.currentIndex - _state.renderedStart
  if (remainingBehind <= 5 && _state.renderedStart > 0) {
    _extendBackwardTo(_state.renderedStart - 15)
  }
  warmNearbyShortVideos(_state.currentIndex)
}

export function appendNewItemsFromGrid() {
  var prevLength = _state.items.length
  var currentCard = _state.cards[_state.currentIndex]
  var deck = deckState()
  var fromCard = _state.cards[deck.fromIndex]
  var targetCard = _state.cards[deck.targetIndex]
  var warmIds = (_state.warmVideoIndexes || []).map(function (index) {
    return _state.items[index] && _state.items[index].data.id
  })
  syncCardList()
  _state.cardIndexById = new Map()
  _state.cards.forEach(function (card, i) {
    var id = String(card.getAttribute('data-video-id') || '').trim()
    if (id) _state.cardIndexById.set(id, i)
  })
  var previousItems = _state.items
  _state.items = _state.cards.map(function (card) {
    return _state.byId.get(String(card.getAttribute('data-video-id') || '').trim()) || null
  })
  _state.warmVideoIndexes = warmIds.map(function (id) { return _state.cardIndexById.get(id) }).filter(function (index) { return index !== undefined })
  previousItems.forEach(function (entry) {
    if (!entry || _state.cardIndexById.has(entry.data.id)) return
    disposeShortItem(entry)
    if (entry.el && entry.el.parentNode) entry.el.parentNode.removeChild(entry.el)
    _state.byId.delete(entry.data.id)
  })
  if (currentCard) _state.currentIndex = _state.cardIndexById.get(currentCard.getAttribute('data-video-id')) ?? -1
  if (deck.phase === 'transitioning') {
    deck.fromIndex = _state.cardIndexById.get(fromCard && fromCard.getAttribute('data-video-id')) ?? -1
    deck.targetIndex = _state.cardIndexById.get(targetCard && targetCard.getAttribute('data-video-id')) ?? -1
    if (deck.fromIndex < 0 || deck.targetIndex < 0) clearDeckTransition()
  }
  _state.cards.forEach(function (card, i) {
    var entry = _state.items[i]
    if (!entry) return
    var data = _fns.parseCardData(card)
    if (data) {
      entry.data.title = data.title
      entry.data.description = data.description
      entry.data.bookmarked = data.bookmarked
      entry.data.bookmarkCategoryId = data.bookmarkCategoryId
    }
  })
  if (_state.currentIndex >= 0) {
    if (_endDistance && _state.currentIndex + 1 < _state.cards.length) setEndDistance(0, true)
    if (_state.storyMode) extendShortsWindow()
    else {
      ensureDeckRange(_state.currentIndex, { skipPosition: deckIsTransitioning() })
      if (deckIsTransitioning()) positionDeckItems(deck.targetIndex, true)
    }
  }
  _fns.updateTopControls()
  updateCurrentActionButtons()
  return _state.cards.length - prevLength
}

export function syncCardList() {
  var root = (_state.storyMode && _state.storySourceContainer) ? _state.storySourceContainer : _dom.sourceContainer
  _state.cards = qa(_dom.sourceCardSelector, root).filter(function (node) {
    return node && node.tagName && String(node.tagName).toLowerCase() === 'a'
  })
}

export function ensureOverlayHydrated() {
  if (_state.overlayHydrated) return
  _state.overlayHydrated = true
  syncCardList()
  _state.cardIndexById = new Map()
  _state.cards.forEach(function (card, i) {
    var id = String(card.getAttribute('data-video-id') || '').trim()
    if (id) _state.cardIndexById.set(id, i)
  })
  _state.items = new Array(_state.cards.length).fill(null)
}

export function ensureContainerScrollBehavior() {
  _dom.shortsContainer.style.display = ''
  _dom.shortsContainer.style.overflowX = 'hidden'
  _dom.shortsContainer.style.overflowY = 'hidden'
  _dom.shortsContainer.style.scrollSnapType = 'none'
  _dom.shortsContainer.style.scrollBehavior = 'auto'
  _dom.shortsContainer.style.webkitOverflowScrolling = 'auto'
  _dom.shortsContainer.style.overscrollBehaviorY = 'contain'
  _dom.shortsContainer.style.overscrollBehaviorX = 'contain'
  _dom.shortsContainer.style.touchAction = 'none'
}

export function ensureStoryContainerScrollBehavior() {
  _dom.shortsContainer.style.display = 'flex'
  _dom.shortsContainer.style.overflowX = 'auto'
  _dom.shortsContainer.style.overflowY = 'hidden'
  _dom.shortsContainer.style.scrollSnapType = 'x mandatory'
  _dom.shortsContainer.style.scrollBehavior = 'smooth'
  _dom.shortsContainer.style.webkitOverflowScrolling = 'touch'
  _dom.shortsContainer.style.overscrollBehaviorX = 'contain'
  _dom.shortsContainer.style.overscrollBehaviorY = 'none'
  _dom.shortsContainer.style.touchAction = 'pan-x'
}

export function showGrid() {
  if (_state.storyMode && _fns && typeof _fns.exitStoryMode === 'function') {
    if (_fns.exitStoryMode({ restore: true }) !== false) return
  }
  setOverlayVisible(false)
}

export function openOverlayAtIndex(index, immediate, options) {
  var opts = options || {}
  ensureOverlayHydrated()
  if (!Number.isFinite(index)) index = 0
  if (index < 0) index = 0
  var total = _state.cards.length
  if (index >= total) index = total - 1
  if (index < 0) return
  var targetCard = _state.cards[index]
  if (hydrateCardAtIndex(index, {
    open: true,
    immediate: immediate !== false,
    persistCursor: opts.persistCursor !== false,
    openRequestSeq: Number(_state.openRequestSeq || 0),
    openVideoId: String(targetCard && targetCard.getAttribute('data-video-id') || '').trim()
  })) return
  var wasOpen = _state.overlayOpen
  renderShortsWindow(index)
  if (!wasOpen && _fns && typeof _fns.beforeOverlayOpen === 'function') _fns.beforeOverlayOpen()
  setOverlayVisible(true)
  activateIndex(index, {
    force: true,
    play: true,
    persist: opts.persistCursor !== false,
    recordView: true
  })
  if (_state.storyMode) ensureCurrentVisible(index, true)
}

export function openOverlayByVideoId(videoId, immediate, options) {
  ensureOverlayHydrated()
  var id = String(videoId || '').trim()
  if (!id) return false
  var cardIndex = _state.cardIndexById.get(id)
  if (cardIndex !== undefined) {
    openOverlayAtIndex(cardIndex, immediate !== false, options)
    return true
  }
  return false
}
