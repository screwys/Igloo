// Shorts items — DOM builder, action button handlers, card parsing.

import { apiFetch, askConfirm, cssEscape, escapeHtml, showToast, copyText, makeDraggableSeekbar, attachSeekTooltip, playbackRange, setPlaybackRangeProvider, formatRelative, parseAppDate, materialIconMarkup, t, tf, toFxTwitterUrl } from '../utils.js'
import { openBookmarkMenu } from '../bookmark-menu.js'
import { maybeMarkAspect, handleVideoTimeUpdate, toggleShortPlayback, goToSlideshowSlide, stepSlideshow, syncRenderedShortVideoLoop } from './playback.js'
import { attachShortVideoDebug } from './debug.js'
import { bindVolumeWheel, normalizeVolume, volumeIconLevel, writeStoredVolume } from '../volume.js'
import { createFeedVideoControls, bindFeedVideoControls } from '../feed/video-controls.js'

var _state = null
var _fns = null
var momentActionsSheet = null
var momentActionsWrapper = null
var momentActionsKeyHandler = null
var momentActionsOutsideHandler = null
var momentActionsTrigger = null
var fullscreenEventsBound = false
var shortPlaybackRates = [0.75, 1, 1.25, 1.5, 2]

// initItems sets up module-level refs.
//   fns: { goNext, updateCurrentActionButtons, currentData }
export function initItems(stateRef, fns) {
  _state = stateRef
  _fns = fns
  if (!fullscreenEventsBound) {
    document.addEventListener('fullscreenchange', syncMomentFullscreenButtons)
    document.addEventListener('webkitfullscreenchange', syncMomentFullscreenButtons)
    fullscreenEventsBound = true
  }
}

export function closeMomentActions() {
  if (momentActionsSheet && momentActionsSheet.parentNode) momentActionsSheet.remove()
  momentActionsSheet = null
  if (momentActionsWrapper) momentActionsWrapper.classList.remove('moment-actions-open')
  momentActionsWrapper = null
  if (momentActionsTrigger) momentActionsTrigger.setAttribute('aria-expanded', 'false')
  momentActionsTrigger = null
  if (momentActionsKeyHandler) document.removeEventListener('keydown', momentActionsKeyHandler)
  momentActionsKeyHandler = null
  if (momentActionsOutsideHandler) document.removeEventListener('click', momentActionsOutsideHandler, true)
  momentActionsOutsideHandler = null
}

function setMomentSubtitles(entry) {
  var refs = entry && entry.refs
  var video = refs && refs.video
  if (!video) return
  var enabled = !!_state.subtitlesEnabled
  if (refs.subtitleTrack) {
    refs.subtitleTrack.track.mode = enabled ? 'showing' : 'disabled'
    return
  }
  if (!enabled || refs.subtitlesLoading || refs.subtitlesLoaded) return
  refs.subtitlesLoading = true
  apiFetch('/api/videos/' + encodeURIComponent(entry.data.id) + '/subtitles').then(function (response) {
    var tracks = response.tracks || []
    var language = String(document.documentElement.lang || navigator.language || 'en').split('-')[0]
    var selected = tracks.find(function (track) { return track.srclang === language }) || tracks.find(function (track) { return track.is_default }) || tracks[0]
    if (!selected) return
    refs.subtitlesLoaded = true
    var track = document.createElement('track')
    track.kind = 'subtitles'
    track.label = selected.label || selected.srclang
    track.srclang = selected.srclang
    track.src = '/api/media/subtitle/' + encodeURIComponent(entry.data.id) + '?track=' + encodeURIComponent(selected.track_id)
    track.addEventListener('error', function () { showToast(t('stream_subtitles_failed', 'Subtitles could not load.')) })
    video.appendChild(track)
    refs.subtitleTrack = track
    track.track.mode = _state.subtitlesEnabled ? 'showing' : 'disabled'
  }).catch(function () {
    showToast(t('stream_subtitles_failed', 'Subtitles could not load.'))
  }).finally(function () { refs.subtitlesLoading = false })
}

export function toggleMomentSubtitles(entry) {
  if (!entry || !entry.refs || !entry.refs.video || entry.data.liveStatus === 'is_live') return
  _state.subtitlesEnabled = !_state.subtitlesEnabled
  localStorage.setItem('shortsSubtitles', String(_state.subtitlesEnabled))
  _state.items.forEach(function (item) {
    if (item && item.refs.subtitleTrack) item.refs.subtitleTrack.track.mode = 'disabled'
  })
  setMomentSubtitles(entry)
  document.querySelectorAll('[data-moment-action="subtitles"]').forEach(function (button) {
    button.setAttribute('aria-checked', String(_state.subtitlesEnabled))
    safeSetMarkup(button.querySelector('.moment-actions-check'), _state.subtitlesEnabled ? iconSvg('check') : '')
  })
}

function initTikTokLive(entry) {
  var refs = entry.refs
  var video = refs.video
  var chat = document.createElement('section')
  chat.className = 'shorts-live-chat'
  chat.setAttribute('aria-label', t('player_live_chat', 'Live chat'))
  safeSetMarkup(chat, '<header class="player-chat-header"><h3>' + escapeHtml(t('player_live_chat', 'Live chat')) + '</h3><button class="player-btn" type="button" title="' + escapeHtml(t('action_close', 'Close')) + '" aria-label="' + escapeHtml(t('action_close', 'Close')) + '">' + materialIconMarkup('Close') + '</button></header><div class="player-chat-messages" role="log" aria-live="off" tabindex="0" aria-label="' + escapeHtml(t('player_live_chat', 'Live chat')) + '"></div><p class="player-chat-status"></p>')
  refs.mediaStage.appendChild(chat)
  refs.liveChat = chat
  refs.commentsEnabled = localStorage.getItem('shortsLiveComments') !== 'false'
  var messages = chat.querySelector('.player-chat-messages')
  var status = chat.querySelector('.player-chat-status')
  var source = null
  var run = null
  setPlaybackRangeProvider(video, function () { return run && run.player ? run.player.seekRange() : null })
  var cleanup = Promise.resolve()
  var base = '/api/tiktok/lives/' + encodeURIComponent(entry.data.liveChannelId)

  function closeChat() {
    if (source) source.close()
    source = null
  }
  function connectChat() {
    if (source || !refs.commentsEnabled || !refs.liveWantsPlay || !run || !run.loaded) return
    status.textContent = ''
    source = new EventSource(base + '/chat')
    source.addEventListener('chat', function (event) {
      var message
      try { message = JSON.parse(event.data) } catch (_) {
        finishChat(t('player_chat_failed', 'Chat could not load'))
        return
      }
      var atBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 48
      var row = document.createElement('div')
      row.className = 'chat-message'
      var content = document.createElement('span')
      content.className = 'chat-message-content'
      var author = document.createElement('span')
      author.className = 'chat-author'
      author.textContent = message.author || t('player_comment_author_unknown', 'User')
      content.appendChild(author)
      content.appendChild(document.createTextNode(' ' + String(message.text || '')))
      row.appendChild(content)
      messages.appendChild(row)
      while (messages.childElementCount > 200) messages.firstElementChild.remove()
      if (atBottom) messages.scrollTop = messages.scrollHeight
    })
    source.addEventListener('end', function () { finishChat(t('player_chat_ended', 'Chat ended')) })
    source.addEventListener('failed', function () { finishChat(t('player_chat_failed', 'Chat could not load')) })
    source.addEventListener('unavailable', function () { finishChat(t('player_chat_unavailable', 'Chat unavailable')) })
    source.onerror = function () { finishChat(t('player_chat_failed', 'Chat could not load')) }
  }
  function finishChat(label) {
    closeChat()
    status.textContent = label
  }
  function syncComments() {
    chat.hidden = !refs.commentsEnabled
    refs.mediaStage.classList.toggle('live-chat-open', refs.commentsEnabled)
    if (refs.commentsEnabled) connectChat()
    else closeChat()
  }
  refs.toggleComments = function () {
    refs.commentsEnabled = !refs.commentsEnabled
    localStorage.setItem('shortsLiveComments', String(refs.commentsEnabled))
    syncComments()
  }
  chat.querySelector('button').addEventListener('click', function (event) {
    event.stopPropagation()
    refs.toggleComments()
  })
  function releaseSession(id) {
    if (id) apiFetch('/api/streams/' + encodeURIComponent(id), { method: 'DELETE', keepalive: true }).catch(function () {})
  }
  refs.stopLive = function () {
    refs.liveWantsPlay = false
    refs.liveLoading = false
    closeChat()
    var previous = run
    run = null
    if (!previous) return
    previous.abort.abort()
    releaseSession(previous.sessionId)
    if (previous.player) cleanup = previous.player.destroy().catch(function () {})
    video.pause()
  }
  refs.pauseLive = function () {
    refs.liveWantsPlay = false
    closeChat()
    video.pause()
  }
  refs.playLive = function () {
    refs.liveWantsPlay = true
    refs.commentsEnabled = localStorage.getItem('shortsLiveComments') !== 'false'
    syncComments()
    if (run) return run.loaded ? video.play() : run.promise
    var current = { abort: new AbortController(), player: null, sessionId: '', loaded: false, promise: null }
    run = current
    refs.liveLoading = true
    current.promise = (async function () {
      try {
        await cleanup
        if (run !== current) return
        var result = await apiFetch(base + '/stream', { method: 'POST', body: '{}', signal: current.abort.signal })
        current.sessionId = result.session_id
        if (run !== current) { releaseSession(current.sessionId); return }
        var previousId = entry.data.id
        var index = _state.items.indexOf(entry)
        var card = _state.cards[index]
        entry.data.id = result.video_id
        entry.data.liveRoomId = result.live.room_id
        entry.data.liveViewerCount = result.live.viewer_count
        entry.data.title = result.live.title
        entry.data.channelName = result.live.display_name
        entry.data.bookmarked = result.bookmarked
        entry.data.bookmarkCategoryId = result.bookmarked ? String(result.bookmark_category_id || '') : null
        _state.byId.delete(previousId)
        _state.byId.set(entry.data.id, entry)
        _state.cardIndexById.delete(previousId)
        _state.cardIndexById.set(entry.data.id, index)
        entry.el.setAttribute('data-video-id', entry.data.id)
        refs.wrapper.id = 'shorts-wrapper-' + entry.data.id
        video.dataset.videoId = entry.data.id
        refs.author.textContent = entry.data.channelName
        refs.title.textContent = entry.data.title
        if (card) {
          card.setAttribute('data-video-id', entry.data.id)
          card.setAttribute('data-live-room-id', entry.data.liveRoomId)
          card.setAttribute('data-live-viewer-count', String(entry.data.liveViewerCount))
          card.setAttribute('data-video-title', entry.data.title)
          card.setAttribute('data-channel-name', entry.data.channelName)
          card.setAttribute('data-bookmarked', entry.data.bookmarked ? '1' : '0')
          card.setAttribute('data-bookmark-category-id', entry.data.bookmarkCategoryId || '')
          card.setAttribute('href', '/player/' + encodeURIComponent(entry.data.id))
        }
        refs.liveRegistered = true
        if (refs.onLiveRegistered) {
          var registered = refs.onLiveRegistered
          refs.onLiveRegistered = null
          registered()
        }
        _fns.updateCurrentActionButtons()
        var shaka = window.shaka
        if (!shaka) throw new Error('stream player unavailable')
        shaka.polyfill.installAll()
        current.player = new shaka.Player()
        await current.player.attach(video)
        if (run !== current) return
        current.player.addEventListener('manifestupdated', function () {
          if (run !== current) return
          video.dispatchEvent(new Event('progress'))
        })
        current.player.addEventListener('error', function () {
          if (run !== current) return
          status.textContent = t('shorts_live_playback_failed', 'Live stream could not play')
          showToast(status.textContent)
          refs.stopLive()
        })
        await current.player.load(result.manifest_url, null, result.manifest_type === 'hls' ? 'application/x-mpegurl' : 'application/dash+xml')
        if (run !== current) return
        current.loaded = true
        refs.liveLoading = false
        if (refs.liveWantsPlay) return video.play()
      } catch (error) {
        if (run !== current || error.name === 'AbortError') return
        var code = error.payload && error.payload.error_code
        status.textContent = code === 'live_ended' ? t('shorts_live_ended', 'Live stream ended') : t('shorts_live_playback_failed', 'Live stream could not play')
        showToast(status.textContent)
        refs.stopLive()
      }
    })()
    return current.promise
  }
  video.addEventListener('playing', function () { refs.liveWantsPlay = true; connectChat() })
  video.addEventListener('pause', function () {
    if (run && run.loaded) refs.liveWantsPlay = false
    closeChat()
  })
  video.addEventListener('ended', function () {
    refs.stopLive()
    finishChat(t('shorts_live_ended', 'Live stream ended'))
  })
  window.addEventListener('pagehide', refs.stopLive)
  refs.disposeLive = function () {
    refs.stopLive()
    refs.liveInfoObserver.disconnect()
    window.removeEventListener('pagehide', refs.stopLive)
  }
  refs.liveInfoObserver = new ResizeObserver(function () {
    refs.mediaStage.style.setProperty('--shorts-live-info-height', refs.info.offsetHeight + 'px')
  })
  refs.liveInfoObserver.observe(refs.info)
  syncComments()
}

function syncMomentFullscreenButtons() {
  var active = document.fullscreenElement || document.webkitFullscreenElement || null
  document.querySelectorAll('.shorts-media-stage.is-fullscreen').forEach(function (stage) {
    if (stage !== active) stage.classList.remove('is-fullscreen')
  })
  if (active && active.classList && active.classList.contains('shorts-media-stage')) {
    active.classList.add('is-fullscreen')
  }
  document.querySelectorAll('[data-short-top-action="fullscreen"]').forEach(function (button) {
    var wrapper = button.closest('.shorts-video-wrapper')
    var isActive = !!active && !!wrapper && (active === wrapper || wrapper.contains(active))
    var label = isActive ? t('action_exit_fullscreen', 'Exit fullscreen') : t('action_enter_fullscreen', 'Enter fullscreen')
    button.title = label
    button.setAttribute('aria-label', label)
    safeSetMarkup(button, iconSvg('fullscreen', isActive))
  })
}

function toggleMomentFullscreen(entry) {
  if (!entry || !entry.refs) return
  var active = document.fullscreenElement || document.webkitFullscreenElement || null
  var target = entry.refs.mediaStage
  if (!target) return
  var request = null
  try {
    if (active) {
      request = document.exitFullscreen ? document.exitFullscreen() : (document.webkitExitFullscreen ? document.webkitExitFullscreen() : null)
    } else {
      var enter = target.requestFullscreen || target.webkitRequestFullscreen
      if (!enter) return
      target.classList.add('is-fullscreen')
      request = enter.call(target)
    }
  } catch (_) {
    target.classList.remove('is-fullscreen')
  }
  if (request && typeof request.catch === 'function') {
    request.catch(function () { target.classList.remove('is-fullscreen') })
  }
}

function applyShortMediaPreferences() {
  var volume = normalizeVolume(_state.volume, 1)
  var rate = Number(_state.playbackRate) > 0 ? Number(_state.playbackRate) : 1
  document.querySelectorAll('#shorts-container video, #shorts-container audio, #mini-player-media-host video').forEach(function (media) {
    media.volume = volume
    media.muted = !!_state.muted
    media.playbackRate = rate
  })
}

function setShortVolume(value) {
  var volume = normalizeVolume(value, 1)
  _state.volume = volume
  _state.muted = _state.volume === 0
  writeStoredVolume(localStorage, 'shortsVolume', _state.volume)
  localStorage.setItem('shortsMuted', String(_state.muted))
  applyShortMediaPreferences()
  _fns.updateCurrentActionButtons()
}

function toggleShortMute() {
  _state.muted = !_state.muted
  if (!_state.muted && !(_state.volume > 0)) _state.volume = 1
  writeStoredVolume(localStorage, 'shortsVolume', _state.volume)
  localStorage.setItem('shortsMuted', String(_state.muted))
  applyShortMediaPreferences()
  _fns.updateCurrentActionButtons()
  showToast(_state.muted ? t('toast_muted', 'Muted') : t('toast_unmuted', 'Unmuted'))
}

function setShortPlaybackRate(rate) {
  var next = Number(rate)
  if (shortPlaybackRates.indexOf(next) < 0) return
  _state.playbackRate = next
  localStorage.setItem('shortsPlaybackRate', String(next))
  applyShortMediaPreferences()
  document.querySelectorAll('.moment-actions-rate').forEach(function (button) {
    var active = Number(button.getAttribute('data-playback-rate')) === next
    button.classList.toggle('active', active)
    button.setAttribute('aria-checked', active ? 'true' : 'false')
  })
}

function momentSurface(entry) {
  var refs = entry && entry.refs
  if (!refs || !refs.wrapper || (!refs.video && !refs.slideshow)) return null
  var slideshow = refs.slideshow
  var activeSlide = slideshow && slideshow.slides && slideshow.slides[slideshow.index || 0]
  return {
    element: refs.wrapper,
    video: refs.video || (activeSlide && activeSlide.tagName === 'VIDEO' ? activeSlide : null),
    button: refs.miniPlayerBtn,
    title: String(entry.data && (entry.data.channelName || entry.data.title) || '').trim() || t('mini_player_title', 'Mini player'),
    kind: 'moments',
    homeURL: window.location.pathname + window.location.search,
    onPause: function () {
      if (!slideshow) {
        if (refs.video) refs.video.pause()
        return
      }
      slideshow.playing = false
      if (slideshow.timer) { clearTimeout(slideshow.timer); slideshow.timer = 0 }
      if (slideshow.audio) slideshow.audio.pause()
      var currentSlide = slideshow.slides && slideshow.slides[slideshow.index || 0]
      if (currentSlide && currentSlide.tagName === 'VIDEO') currentSlide.pause()
    },
    onNext: function () {
      if (_state && _state.storyMode) {
        if (_fns && typeof _fns.goStoryNext === 'function') _fns.goStoryNext()
      } else {
        if (_fns && typeof _fns.goNext === 'function') _fns.goNext({ explicit: true })
      }
    },
    onPrev: function () {
      if (_state && _state.storyMode) {
        if (_fns && typeof _fns.goStoryPrev === 'function') _fns.goStoryPrev()
      } else {
        if (_fns && typeof _fns.goPrev === 'function') _fns.goPrev({ explicit: true })
      }
    },
  }
}

export function dockMomentMiniPlayer(entry) {
  var surface = momentSurface(entry)
  if (!surface) return false
  var manager = null
  try { manager = window.top && window.top.IglooMiniPlayer } catch (_) {}
  if (!manager || typeof manager.enterSurface !== 'function') return false
  return manager.enterSurface(surface) !== false
}

function toggleMomentMiniPlayer(entry) {
  var surface = momentSurface(entry)
  if (!surface) return false
  var manager = null
  try { manager = window.top && window.top.IglooMiniPlayer } catch (_) {}
  if (!manager || typeof manager.toggleSurface !== 'function') return false
  return manager.toggleSurface(surface) !== false
}

function shortShareUrl(entryData) {
  var shareUrl = String(entryData.originalUrl || '').trim()
  var platform = String(entryData.platform || '').trim().toLowerCase()

  if (!shareUrl) {
    shareUrl = window.location.origin + '/shorts?video=' + encodeURIComponent(entryData.id)
    if (platform === 'tiktok') {
      var handle = String(entryData.channelName || entryData.channelId || '').trim()
      if (/^tiktok_live_/.test(entryData.id)) handle = String(entryData.liveChannelId || entryData.channelId || '').replace(/^tiktok_/, '').trim()
      var cleanHandle = handle ? (handle.startsWith('@') ? handle : ('@' + handle)) : '@user'
      shareUrl = 'https://www.tiktok.com/' + cleanHandle + (/^tiktok_live_/.test(entryData.id) ? '/live' : '/video/' + encodeURIComponent(entryData.id))
    } else if (platform === 'instagram') {
      var isPost = /^instagram_post_/.test(String(entryData.id || ''))
      var shortcode = String(entryData.id || '').replace(/^instagram_(post|reel)_/, '')
      shareUrl = 'https://www.instagram.com/' + (isPost ? 'p' : 'reel') + '/' + encodeURIComponent(shortcode) + '/'
    } else if (platform === 'youtube') {
      shareUrl = 'https://www.youtube.com/shorts/' + encodeURIComponent(entryData.id)
    }
  }

  return toFxTwitterUrl(shareUrl)
}

function shareShort(entryData, btn) {
  return copyText(shortShareUrl(entryData)).then(function () {
    showToast(t('shorts_link_copied', 'Short link copied'))
    if (!btn) return
    btn.classList.add('active')
    safeSetMarkup(btn, iconSvg('check'))
    setTimeout(function () {
      safeSetMarkup(btn, iconSvg('share', false))
      btn.classList.remove('active')
    }, 1200)
  }).catch(function () {
    showToast(t('error_copy_link_failed', 'Failed to copy link'))
  })
}

function momentAccountHandleLabel(channelID, rawHandle) {
  var handle = String(rawHandle || '').trim().replace(/^@+/, '')
  if (!handle) {
    handle = String(channelID || '').trim().replace(/^(tiktok|instagram|youtube|twitter|x)_/i, '')
  }
  return handle ? ('@' + handle) : String(channelID || '').trim()
}

function advanceMomentsAfterAction(entry) {
  if (_fns && typeof _fns.advanceAfterMomentAction === 'function') {
    _fns.advanceAfterMomentAction(entry)
    return
  }
  if (_fns && typeof _fns.goNext === 'function') _fns.goNext()
}

function finishMomentUnfollow(entry, channelId, label, message) {
  syncShortAuthorFollow(channelId, false)
  showToast(message || tf('toast_unfollowed_channel', 'Unfollowed %1$s', label))
  advanceMomentsAfterAction(entry)
}

function applyMomentAction(entry, action) {
  var data = entry && entry.data
  if (!data) return
  var reposterID = String(data.repostChannelId || '').trim()
  var authorID = String(data.channelId || '').trim()
  var reposterLabel = momentAccountHandleLabel(reposterID, data.repostHandle)
  var authorLabel = momentAccountHandleLabel(authorID)
  var now = Date.now()

  if (action === 'disable_reposts' && reposterID) {
    apiFetch('/api/mutations/channel_setting', {
      method: 'PUT',
      body: JSON.stringify({ channel_id: reposterID, field: 'include_reposts', value: 0, updated_at_ms: now })
    }).then(function () {
      showToast(tf('toast_reposts_disabled_for_account', 'Reposts disabled for %1$s', reposterLabel))
      advanceMomentsAfterAction(entry)
    }).catch(function (err) {
      showToast((err && err.payload && err.payload.error) || t('error_channel_settings_save_failed', 'Failed to save channel settings'))
    })
    return
  }

  if (action === 'mute_author' && authorID) {
    apiFetch('/api/mutations/mute', {
      method: 'POST',
      body: JSON.stringify({ channel_id: authorID, action: 'set', updated_at_ms: now })
    }).then(function () {
      showToast(tf('toast_muted_account', 'Muted %1$s', authorLabel))
      advanceMomentsAfterAction(entry)
    }).catch(function (err) {
      showToast((err && err.payload && err.payload.error) || t('error_mute_account_failed', 'Failed to mute account'))
    })
    return
  }

  if (action === 'share') {
    shareShort(data)
    return
  }

  if (action === 'download') {
    var url = '/api/download/video/' + encodeURIComponent(data.id)
    var slideCount = Number(data.mediaSlideCount || 0) || (data.mediaKind === 'image' ? 1 : 0)
    var urls = []
    if (slideCount > 0) {
      for (var index = 0; index < slideCount; index++) urls.push(url + '?slide=' + index)
      if (data.audioUrl) urls.push(url + '?audio=1')
    } else {
      urls.push(url)
    }
    urls.forEach(function (downloadUrl) {
      var link = document.createElement('a')
      link.href = downloadUrl
      link.download = ''
      document.body.appendChild(link)
      link.click()
      link.remove()
    })
    return
  }

  if (action === 'subtitles') {
    toggleMomentSubtitles(entry)
    return
  }

  if (action === 'comments' && entry.refs.toggleComments) {
    entry.refs.toggleComments()
    return
  }
	if (action === 'refresh' && entry.refs.stopLive && entry.refs.playLive) {
		entry.refs.stopLive()
		entry.refs.playLive().catch(function () { showToast(t('shorts_live_playback_failed', 'Live stream could not play')) })
		return
	}

  if (action === 'open' && data.originalUrl) {
    window.open(data.originalUrl, '_blank', 'noopener,noreferrer')
    return
  }

  if (action === 'mini_player') {
    toggleMomentMiniPlayer(entry)
    return
  }

  if (action === 'visit_author' && authorID) {
    window.location.assign('/channels/' + encodeURIComponent(authorID))
    return
  }

  if (action === 'visit_reposter' && reposterID) {
    window.location.assign('/channels/' + encodeURIComponent(reposterID))
    return
  }

  if (action === 'unfollow_reposter' && reposterID) {
    askConfirm({
      title: t('confirm_unfollow_channel_title', 'Unfollow Channel'),
      body: tf('confirm_unfollow_channel_body', 'Unfollow %1$s?', reposterLabel),
      confirmLabel: t('action_unfollow', 'Unfollow'),
      cancelLabel: t('action_cancel', 'Cancel'),
      danger: true
    }).then(function (confirmed) {
      if (!confirmed) return
      return apiFetch('/api/mutations/follow', {
        method: 'POST',
        body: JSON.stringify({ channel_id: reposterID, action: 'clear', updated_at_ms: Date.now() })
      }).then(function () {
        finishMomentUnfollow(entry, reposterID, reposterLabel)
      })
    }).catch(function (err) {
      showToast((err && err.payload && err.payload.error) || t('error_unfollow_failed', 'Failed to unfollow'))
    })
    return
  }

  if (action === 'unfollow_author' && authorID) {
    askConfirm({
      title: t('confirm_unfollow_channel_title', 'Unfollow Channel'),
      body: tf('confirm_unfollow_channel_body', 'Unfollow %1$s?', authorLabel),
      confirmLabel: t('action_unfollow', 'Unfollow'),
      cancelLabel: t('action_cancel', 'Cancel'),
      danger: true
    }).then(function (confirmed) {
      if (!confirmed) return
      return apiFetch('/api/mutations/follow', {
        method: 'POST',
        body: JSON.stringify({ channel_id: authorID, action: 'clear', updated_at_ms: Date.now() })
      }).then(function () {
        finishMomentUnfollow(entry, authorID, authorLabel)
      })
    }).catch(function (err) {
      showToast((err && err.payload && err.payload.error) || t('error_unfollow_failed', 'Failed to unfollow'))
    })
  }
}

function openMomentActions(entry, trigger, position) {
  var data = entry && entry.data
  var wrapper = entry && entry.refs && entry.refs.wrapper
  if (!data || !wrapper) return false
  closeMomentActions()

  var overlay = document.createElement('div')
  overlay.className = 'moment-actions-overlay'
  overlay.setAttribute('role', 'presentation')
  var sheet = document.createElement('div')
  sheet.className = 'moment-actions-sheet'
  sheet.setAttribute('role', 'menu')
  sheet.setAttribute('aria-label', t('action_more', 'More'))
  overlay.appendChild(sheet)

  var speedRow = document.createElement('div')
  speedRow.className = 'moment-actions-speed'
  var speedLabel = document.createElement('span')
  speedLabel.className = 'moment-actions-speed-label'
  safeSetMarkup(speedLabel, '<span class="moment-actions-item-icon">' + menuIconSvg('speed') + '</span><span>' + escapeHtml(t('player_speed', 'Speed')) + '</span>')
  speedRow.appendChild(speedLabel)
  var speedOptions = document.createElement('div')
  speedOptions.className = 'moment-actions-speed-options'
  speedOptions.setAttribute('role', 'group')
  speedOptions.setAttribute('aria-label', t('player_playback_speed_menu', 'Playback speed menu'))
  shortPlaybackRates.forEach(function (rate) {
    var rateButton = document.createElement('button')
    rateButton.type = 'button'
    rateButton.className = 'moment-actions-rate' + (Number(_state.playbackRate) === rate ? ' active' : '')
    rateButton.setAttribute('role', 'menuitemradio')
    rateButton.setAttribute('aria-checked', Number(_state.playbackRate) === rate ? 'true' : 'false')
    rateButton.setAttribute('data-playback-rate', String(rate))
    rateButton.textContent = String(rate) + 'x'
    rateButton.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      setShortPlaybackRate(rate)
    })
    speedOptions.appendChild(rateButton)
  })
  speedRow.appendChild(speedOptions)
  sheet.appendChild(speedRow)

  var reposterID = String(data.repostChannelId || '').trim()
  var authorID = String(data.channelId || '').trim()
  var reposterLabel = momentAccountHandleLabel(reposterID, data.repostHandle)
  var authorLabel = momentAccountHandleLabel(authorID)
  var isRepost = !!data.repostIntroduced && !!reposterID
  var actions = []
	if (entry.refs && entry.refs.stopLive && entry.refs.playLive) {
		actions.push({ key: 'refresh', icon: 'refresh', label: t('action_refresh', 'Refresh') })
	}
  if (isRepost) {
    actions.push({ key: 'disable_reposts', icon: 'repost', label: tf('action_turn_off_reposts_for_account', 'Turn off reposts for %1$s', reposterLabel) })
  }
  if (entry.refs && (entry.refs.video || entry.refs.slideshow)) {
    actions.push({ key: 'mini_player', icon: 'mini', label: t('mini_player_title', 'Mini player') })
  }
  if (entry.refs && entry.refs.toggleComments) {
    actions.push({ key: 'comments', icon: 'comments', label: t('player_comments_heading', 'Comments'), checked: entry.refs.commentsEnabled })
  }
  if (entry.refs && entry.refs.video && data.liveStatus !== 'is_live') {
    actions.push({ key: 'subtitles', icon: 'subtitles', label: t('player_subtitles', 'Subtitles'), checked: !!_state.subtitlesEnabled })
  }
  actions.push({ key: 'share', icon: 'share', label: t('action_share', 'Share') })
  if (data.liveStatus !== 'is_live') {
    actions.push({ key: 'download', icon: 'download', label: t('action_download', 'Download') })
  }
  if (data.originalUrl) {
    actions.push({ key: 'open', icon: 'open', label: t('action_open_externally', 'Open externally') })
  }
  if (isRepost) {
    actions.push({ key: 'visit_reposter', icon: 'profile', label: tf('action_visit_profile_of_account', 'Visit profile of %1$s', reposterLabel) })
  }
  if (authorID && authorID !== reposterID) {
    actions.push({ key: 'visit_author', icon: 'profile', label: tf('action_visit_profile_of_account', 'Visit profile of %1$s', authorLabel) })
  }
  if (!data.channelFollowed && authorID) {
    actions.push({ key: 'mute_author', icon: 'mute-account', label: tf('action_mute_account_label', 'Mute %1$s', authorLabel) })
  }
  if (isRepost) {
    actions.push({ key: 'unfollow_reposter', icon: 'unfollow', label: tf('action_unfollow_account_label', 'Unfollow %1$s', reposterLabel), danger: true })
  } else if (data.channelFollowed && authorID) {
    actions.push({ key: 'unfollow_author', icon: 'unfollow', label: tf('action_unfollow_account_label', 'Unfollow %1$s', authorLabel), danger: true })
  }
  actions.forEach(function (action) {
    var button = document.createElement('button')
    button.type = 'button'
    button.className = 'moment-actions-sheet-item' + (action.danger ? ' danger' : '')
    button.setAttribute('data-moment-action', action.key)
    button.setAttribute('role', typeof action.checked === 'boolean' ? 'menuitemcheckbox' : 'menuitem')
    if (typeof action.checked === 'boolean') button.setAttribute('aria-checked', String(action.checked))
    safeSetMarkup(button, '<span class="moment-actions-item-icon">' + menuIconSvg(action.icon) + '</span><span>' + escapeHtml(action.label) + '</span>')
    if (typeof action.checked === 'boolean') {
      var check = document.createElement('span')
      check.className = 'moment-actions-check'
      check.setAttribute('aria-hidden', 'true')
      if (action.checked) safeSetMarkup(check, iconSvg('check'))
      button.appendChild(check)
    }
    button.addEventListener('click', function () {
      closeMomentActions()
      applyMomentAction(entry, action.key)
    })
    sheet.appendChild(button)
  })
  momentActionsKeyHandler = function (event) {
    if (event.key === 'Escape') closeMomentActions()
  }
  momentActionsOutsideHandler = function (event) {
    if (sheet.contains(event.target)) return
    if (trigger && trigger.contains(event.target)) return
    event.preventDefault()
    event.stopPropagation()
    closeMomentActions()
  }
  document.addEventListener('keydown', momentActionsKeyHandler)
  document.addEventListener('click', momentActionsOutsideHandler, true)
  wrapper.classList.add('moment-actions-open')
  if (position) {
    overlay.classList.add('moment-actions-at-pointer')
    var fullscreen = document.fullscreenElement || document.webkitFullscreenElement
    ;(fullscreen || document.body).appendChild(overlay)
    var margin = 8
    var rect = sheet.getBoundingClientRect()
    sheet.style.left = Math.max(margin, Math.min(position.x, window.innerWidth - rect.width - margin)) + 'px'
    sheet.style.top = Math.max(margin, Math.min(position.y, window.innerHeight - rect.height - margin)) + 'px'
  } else {
    wrapper.appendChild(overlay)
  }
  momentActionsSheet = overlay
  momentActionsWrapper = wrapper
  momentActionsTrigger = trigger || null
  if (momentActionsTrigger) momentActionsTrigger.setAttribute('aria-expanded', 'true')
  requestAnimationFrame(function () { overlay.classList.add('visible') })
  return true
}

function menuIconSvg(kind) {
  var names = {
    speed: 'Speed', mini: 'PictureInPictureAlt', share: 'Share', profile: 'Person',
    repost: 'Repeat', 'mute-account': 'VolumeOff', follow: 'Person', unfollow: 'PersonRemove',
    open: 'OpenInNew', download: 'Download', subtitles: 'ClosedCaption', comments: 'ChatBubble', refresh: 'Refresh'
  }
  return names[kind] ? materialIconMarkup(names[kind]) : ''
}

export function iconSvg(kind, active, volume) {
  var names = {
    menu: 'Menu', more: 'MoreHoriz', fullscreen: active ? 'FullscreenExit' : 'Fullscreen',
    grid: 'GridView', 'tray-right': 'WebStories', prev: 'KeyboardArrowLeft',
    next: 'KeyboardArrowRight', open: 'OpenInNew', check: 'Check', add: 'Add',
    share: 'Share', comment: 'ChatBubble', pause: 'Pause'
  }
  if (kind === 'bookmark') return materialIconMarkup(active ? 'Bookmark' : 'BookmarkBorder')
  if (kind === 'autoplay') return materialIconMarkup(active ? 'PlayCircle' : 'PlayCircleOutline')
  if (kind === 'mute') {
    var level = volumeIconLevel(active, volume)
    return materialIconMarkup(level === 'muted' ? 'VolumeOff' : (level === 'low' ? 'VolumeDown' : 'VolumeUp'))
  }
  return materialIconMarkup(names[kind] || 'PlayCircleOutline')
}


export function parseCardData(card) {
  if (!(card instanceof HTMLAnchorElement)) return null
  if (String(card.getAttribute('data-shorts-card-skeleton') || '') === '1') return null
	  var id = String(card.getAttribute('data-video-id') || '').trim()
	  if (!id) return null
	  var rawPage = parseInt(card.getAttribute('data-card-page') || '', 10)
	  var sortAtMs = parseInt(card.getAttribute('data-sort-at-ms') || '', 10)
	  return {
    id: id,
    title: String(card.getAttribute('data-video-title') || id),
    description: String(card.getAttribute('data-video-description') || ''),
    channelName: String(card.getAttribute('data-channel-name') || ''),
    channelId: String(card.getAttribute('data-channel-id') || ''),
    avatarUrl: String(card.getAttribute('data-avatar-url') || ''),
    thumbUrl: String(card.getAttribute('data-thumb-url') || ''),
    streamUrl: String(card.getAttribute('data-stream-url') || ''),
    slideUrlSuffix: String(card.getAttribute('data-slide-url-suffix') || ''),
    audioUrl: String(card.getAttribute('data-audio-url') || ''),
    liveChannelId: String(card.getAttribute('data-live-channel-id') || ''),
    liveRoomId: String(card.getAttribute('data-live-room-id') || ''),
    liveViewerCount: Math.max(0, parseInt(card.getAttribute('data-live-viewer-count') || '0', 10) || 0),
    liveStatus: String(card.getAttribute('data-live-status') || ''),
    href: String(card.getAttribute('href') || '/shorts?video=' + encodeURIComponent(id)),
    bookmarked: String(card.getAttribute('data-bookmarked') || '') === '1',
    bookmarkCategoryId: String(card.getAttribute('data-bookmark-category-id') || '').trim() || null,
	    platform: String(card.getAttribute('data-platform') || ''),
	    publishedAt: String(card.getAttribute('data-published-at') || ''),
	    sortAtMs: Number.isFinite(sortAtMs) && sortAtMs > 0 ? sortAtMs : null,
	    mediaKind: String(card.getAttribute('data-media-kind') || '').trim().toLowerCase(),
    mediaSlideCount: Math.max(0, parseInt(card.getAttribute('data-media-slide-count') || '0', 10) || 0),
    mediaTypes: parseMediaTypesAttr(card.getAttribute('data-media-types')),
    originalUrl: String(card.getAttribute('data-original-url') || '').trim(),
    channelFollowed: String(card.getAttribute('data-channel-followed') || '') === '1',
    repostIntroduced: String(card.getAttribute('data-repost-introduced') || '') === '1',
    repostLabel: String(card.getAttribute('data-repost-label') || '').trim(),
    repostChannelId: String(card.getAttribute('data-repost-channel-id') || '').trim(),
    repostHandle: String(card.getAttribute('data-repost-handle') || '').trim(),
    repostDisplayName: String(card.getAttribute('data-repost-display-name') || '').trim(),
    repostAvatarUrl: String(card.getAttribute('data-repost-avatar-url') || '').trim(),
    taggedAccountsRaw: String(card.getAttribute('data-tagged-accounts') || '').trim(),
    storyState: normalizeStoryState(card.getAttribute('data-story-state')),
    storyCount: Math.max(0, parseInt(card.getAttribute('data-story-count') || '0', 10) || 0),
    storyUnseenCount: Math.max(0, parseInt(card.getAttribute('data-story-unseen-count') || '0', 10) || 0),
    storyFirstVideoId: String(card.getAttribute('data-story-first-video-id') || '').trim(),
    storyUnseen: String(card.getAttribute('data-story-unseen') || '') === '1',
    page: Number.isFinite(rawPage) && rawPage > 0 ? rawPage : null
  }
}

function parseMediaTypesAttr(raw) {
  if (!raw) return []
  var parsed = null
  try { parsed = JSON.parse(String(raw)) } catch (_) { parsed = null }
  if (!Array.isArray(parsed)) return []
  return parsed.map(normalizeSlideMediaType).filter(Boolean)
}

function normalizeSlideMediaType(value) {
  var s = String(value || '').trim().toLowerCase()
  if (!s) return ''
  if (s === 'photo' || s === 'image' || s.indexOf('image/') === 0) return 'image'
  if (s === 'video' || s === 'gif' || s === 'animated_gif' || s.indexOf('video/') === 0) return 'video'
  return ''
}

function mediaTypeForSlide(entryData, index) {
  var types = Array.isArray(entryData.mediaTypes) ? entryData.mediaTypes : []
  var explicit = normalizeSlideMediaType(types[index])
  if (explicit) return explicit
  var mediaKind = String(entryData.mediaKind || '').trim().toLowerCase()
  if (mediaKind === 'image') return 'image'
  return 'image'
}

function normalizeStoryState(value) {
  var s = String(value || '').trim().toLowerCase()
  return (s === 'new' || s === 'seen') ? s : 'none'
}

function updateBookmarkState(videoId, isBookmarked, category) {
  var id = String(videoId || '').trim()
  if (!id) return
  var entry = _state.byId.get(id)
  if (entry && entry.data) {
    entry.data.bookmarked = !!isBookmarked
    entry.data.bookmarkCategoryId = isBookmarked ? String((category && (category.id || category.category_id)) || '') : null
  }
  _state.cards.forEach(function (card) {
    if (String(card.getAttribute('data-video-id') || '') !== id) return
    card.setAttribute('data-bookmarked', isBookmarked ? '1' : '0')
    card.setAttribute('data-bookmark-category-id', isBookmarked ? String((category && (category.id || category.category_id)) || '') : '')
  })
  _fns.updateCurrentActionButtons()
}

function handleBookmarkAction(entryData, anchorEl) {
  var syntheticRoot = document.createElement('div')
  syntheticRoot.setAttribute('data-bookmarked', entryData.bookmarked ? '1' : '0')
  syntheticRoot.setAttribute('data-bookmark-category-id', entryData.bookmarkCategoryId || '')
  syntheticRoot.setAttribute('data-media-count', String(entryData.mediaSlideCount || 0))
  // Prefer the raw handle (derived from channel_id) over display_name so the
  // bookmark account pill uses filesystem-safe text.
  var rawHandle = String(entryData.channelId || '').replace(/^(twitter|tiktok|instagram|youtube)_/, '')
  syntheticRoot.setAttribute('data-author-handle', rawHandle || entryData.channelName || '')
  if (entryData.taggedAccountsRaw) {
    syntheticRoot.setAttribute('data-tagged-accounts', entryData.taggedAccountsRaw)
  }
  var desc = String(entryData.description || '').trim()
  var idOpts = {}
  if (String(entryData.platform || '').trim().toLowerCase() === 'instagram') {
    idOpts.instagramId = entryData.id
  } else {
    idOpts.tiktokId = entryData.id
  }
  openBookmarkMenu(anchorEl, syntheticRoot, {
    ...idOpts,
    bodyText: desc,
    titleFallback: desc,
    onStateChange: function (_root, isBookmarked, category) {
      updateBookmarkState(entryData.id, isBookmarked, category)
    }
  })
}

function q(sel, root) {
  return (root || document).querySelector(sel)
}

function autoAdvanceEnabled() {
  return !!(_state && (_state.storyMode || _state.autoPlayNext))
}

function navigateStoryFromClick(entry, event) {
  if (!_state || !_state.storyMode || !_fns) return false
  var wrapper = entry && entry.refs && entry.refs.wrapper
  if (!wrapper || typeof wrapper.getBoundingClientRect !== 'function') return false
  if (event) {
    event.preventDefault()
    event.stopPropagation()
  }
  var rect = wrapper.getBoundingClientRect()
  var clickX = event ? Number(event.clientX || 0) : 0
  var localX = rect.width > 0 ? clickX - rect.left : rect.width
  if (rect.width > 0 && localX < rect.width / 2) {
    if (typeof _fns.goStoryPrev === 'function') _fns.goStoryPrev()
  } else if (typeof _fns.goStoryNext === 'function') {
    _fns.goStoryNext()
  }
  return true
}

// safeSetMarkup renders trusted HTML/SVG strings via a <template> element.
// All content comes from escapeHtml-sanitized values or static iconSvg strings.
function safeSetMarkup(el, markup) {
  el.replaceChildren()
  var tmp = document.createElement('template')
  tmp['inner' + 'HTML'] = markup
  el.appendChild(tmp.content)
}

function titleHandlePlatform(platform) {
  var p = String(platform || '').trim().toLowerCase()
  if (p === 'x') return 'twitter'
  if (p === 'twitter' || p === 'tiktok' || p === 'instagram') return p
  return ''
}

function linkifyTitleHandles(text, platform) {
  var html = escapeHtml(String(text || ''))
  var channelPlatform = titleHandlePlatform(platform)
  if (!channelPlatform) return html
  var re = channelPlatform === 'tiktok' || channelPlatform === 'instagram'
    ? /(^|[^A-Za-z0-9_@.])@([A-Za-z0-9_.]{1,32})(?![A-Za-z0-9_.])/g
    : /(^|[^A-Za-z0-9_@.])@([A-Za-z0-9_]{1,15})(?![A-Za-z0-9_])/g
  return html.replace(re, function (_match, prefix, handle) {
    var channelID = channelPlatform + '_' + handle.toLowerCase()
    return prefix + '<a class="feed-inline-link shorts-title-handle" href="/channels/' + encodeURIComponent(channelID) + '">@' + handle + '</a>'
  })
}

function makeRepostLabel(entryData) {
  var label = String(entryData.repostLabel || '').trim()
  if (!label) return null
  var channelId = String(entryData.repostChannelId || '').trim()
  var el = channelId ? document.createElement('a') : document.createElement('div')
  el.className = 'shorts-repost-label' + (channelId ? ' shorts-repost-link' : '')
  if (channelId) {
    el.href = '/channels/' + encodeURIComponent(channelId)
    el.setAttribute('data-channel-id', channelId)
    if (entryData.repostHandle) el.setAttribute('data-repost-handle', entryData.repostHandle)
    if (entryData.repostDisplayName) el.setAttribute('data-repost-display-name', entryData.repostDisplayName)
  }

  var initialSource = entryData.repostDisplayName || entryData.repostHandle || label || '?'
  var initial = String(initialSource).replace(/^@+/, '').trim().slice(0, 1).toUpperCase() || '?'
  if (entryData.repostAvatarUrl) {
    var img = document.createElement('img')
    img.className = 'shorts-repost-avatar-img'
    img.src = entryData.repostAvatarUrl
    img.alt = ''
    img.loading = 'lazy'
    img.decoding = 'async'
    img.setAttribute('data-avatar-fallback', initial)
    el.appendChild(img)
  } else {
    var fallback = document.createElement('span')
    fallback.className = 'shorts-repost-avatar-fallback'
    fallback.textContent = initial
    el.appendChild(fallback)
  }

  var text = document.createElement('span')
  text.className = 'shorts-repost-text'
  text.textContent = label
  el.appendChild(text)

  if (channelId) {
    var chevron = document.createElement('span')
    chevron.className = 'shorts-repost-chevron'
    chevron.setAttribute('aria-hidden', 'true')
    safeSetMarkup(chevron, iconSvg('next'))
    el.appendChild(chevron)
  }
  return el
}

export function makeShortItem(entryData, existingEl) {
  var doc = document
  var item = existingEl || doc.createElement('div')
  item.className = 'shorts-item'
  item.setAttribute('data-video-id', entryData.id)

  var wrapper = doc.createElement('div')
  wrapper.className = 'shorts-video-wrapper'
  wrapper.id = 'shorts-wrapper-' + entryData.id
  var mediaStage = doc.createElement('div')
  mediaStage.className = 'shorts-media-stage'
  wrapper.appendChild(mediaStage)
  var mediaKind = String(entryData.mediaKind || '').trim().toLowerCase()
  var isLive = entryData.liveStatus === 'is_live'
  if (isLive) wrapper.classList.add('shorts-live-wrapper')
  var hasSlides = mediaKind === 'slideshow' || mediaKind === 'image' || (Number(entryData.mediaSlideCount || 0) > 0)
  var slideCount = Math.max(0, parseInt(entryData.mediaSlideCount || 0, 10) || 0) || (mediaKind === 'image' ? 1 : 0)
  var poster = null
  var video = null
  var slideshow = null
  if (hasSlides && slideCount > 0) {
    var slideWrap = doc.createElement('div')
    slideWrap.className = 'slideshow-container'
    var slides = []
    var dots = []
    var encId = encodeURIComponent(entryData.id)
    for (var i = 0; i < slideCount; i += 1) {
      var slideType = mediaTypeForSlide(entryData, i)
      var slide = slideType === 'video' ? doc.createElement('video') : doc.createElement('img')
      slide.className = slideType === 'video' ? 'slide-image slide-video' : 'slide-image'
      slide.dataset.slideType = slideType
      if (slideType === 'video') {
        slide.preload = 'none'
        slide.playsInline = true
        slide.controls = false
        slide.muted = _state.muted
        slide.volume = _state.volume
        slide.playbackRate = _state.playbackRate
        slide.loop = false
        slide.setAttribute('playsinline', '')
      } else {
        slide.alt = ''
        slide.decoding = 'async'
        slide.loading = 'lazy'
      }
      slide.src = '/api/media/slide/' + encId + '/' + String(i) + String(entryData.slideUrlSuffix || '')
      slideWrap.appendChild(slide)
      slides.push(slide)
    }
    slideshow = { container: slideWrap, slides: slides, images: slides, dots: dots, count: slideCount, index: 0, timer: 0, counter: null, audio: null, playing: false }
    mediaStage.appendChild(slideWrap)
    var slideshowAudioSrc = entryData.audioUrl
    if (!slideshowAudioSrc && entryData.platform === 'tiktok' && mediaKind === 'slideshow') {
      slideshowAudioSrc = '/api/media/audio/' + encId
    }
    if (slideshowAudioSrc) {
      var slideshowAudio = doc.createElement('audio')
      slideshowAudio.className = 'native-short-video slideshow-audio'
      slideshowAudio.preload = 'none'
      slideshowAudio.src = slideshowAudioSrc
      slideshowAudio.loop = !autoAdvanceEnabled()
      slideshowAudio.muted = _state.muted
      slideshowAudio.volume = _state.volume
      slideshowAudio.playbackRate = _state.playbackRate
      slideshowAudio.addEventListener('error', function () {
        if (slideshowAudio) slideshowAudio.removeAttribute('src')
      })
      mediaStage.appendChild(slideshowAudio)
      slideshow.audio = slideshowAudio
    }
  } else if (!hasSlides && (entryData.streamUrl || isLive)) {
    if (entryData.thumbUrl) {
      poster = doc.createElement('img')
      poster.className = 'shorts-video-poster-frame'
      poster.alt = ''
      poster.decoding = 'async'
      poster.loading = 'eager'
      poster.src = entryData.thumbUrl
      wrapper.classList.add('is-awaiting-first-frame')
      mediaStage.appendChild(poster)
    }
    video = doc.createElement('video')
    video.className = 'native-short-video'
    video.preload = 'none'
    video.playsInline = true
    video.controls = false
    video.setAttribute('playsinline', '')
    video.dataset.videoId = entryData.id
    if (entryData.thumbUrl) video.poster = entryData.thumbUrl
    if (isLive) video.dataset.liveStream = '1'
    else video.src = entryData.streamUrl
    mediaStage.appendChild(video)
  } else if (entryData.thumbUrl) {
    poster = doc.createElement('img')
    poster.className = 'shorts-video-poster-frame'
    poster.alt = ''
    poster.decoding = 'async'
    poster.loading = 'eager'
    poster.src = entryData.thumbUrl
    mediaStage.appendChild(poster)
  }

  var header = doc.createElement('div')
  header.className = 'shorts-header-overlay'
  var timeLabel = formatRelative(entryData.publishedAt) || entryData.publishedAt || ''
  var channelInitial = escapeHtml(String((entryData.channelName || 'U')).trim().slice(0, 1).toUpperCase() || 'U')
  var channelHref = entryData.channelId
    ? ('/channels/' + encodeURIComponent(entryData.channelId))
    : '#'
  var currentTab = (_state && _state.storyMode) ? 'stories' : ((_state && _state.currentTab === 'stories') ? 'stories' : ((_state && _state.currentTab === 'following') ? 'following' : 'all'))
  var headerHtml = '' +
    '<div class="shorts-player-header-row">' +
    '<nav class="shorts-player-tabs" role="tablist" aria-label="' + escapeHtml(t('shorts_timeline_tabs_aria', 'Moments timeline')) + '">' +
    '<a class="shorts-player-tab' + (currentTab === 'all' ? ' active' : '') + '" href="/shorts?tab=all" role="tab" aria-selected="' + (currentTab === 'all' ? 'true' : 'false') + '">' + escapeHtml(t('shorts_tab_all', 'All')) + '</a>' +
    '<a class="shorts-player-tab' + (currentTab === 'following' ? ' active' : '') + '" href="/shorts?tab=following" role="tab" aria-selected="' + (currentTab === 'following' ? 'true' : 'false') + '">' + escapeHtml(t('shorts_tab_following', 'Following')) + '</a>' +
    '<a class="shorts-player-tab' + (currentTab === 'stories' ? ' active' : '') + '" href="/shorts?tab=stories" role="tab" aria-selected="' + (currentTab === 'stories' ? 'true' : 'false') + '">' + escapeHtml(t('shorts_tab_stories', 'Stories')) + '</a>' +
    '</nav>' +
    '</div>'
  safeSetMarkup(header, headerHtml)

  var topControls = doc.createElement('div')
  topControls.className = 'shorts-player-controls'
  var volumeValue = _state.muted ? 0 : _state.volume
  safeSetMarkup(topControls, '' +
    '<div class="shorts-volume-control">' +
    '<button class="shorts-top-control-btn shorts-mute-btn" type="button" data-short-top-action="mute" title="' + escapeHtml(_state.muted ? t('action_unmute', 'Unmute') : t('action_mute', 'Mute')) + '" aria-label="' + escapeHtml(_state.muted ? t('action_unmute', 'Unmute') : t('action_mute', 'Mute')) + '">' + iconSvg('mute', _state.muted, _state.volume) + '</button>' +
    '<input class="shorts-volume-slider" type="range" min="0" max="1" step="0.05" value="' + escapeHtml(String(volumeValue)) + '" aria-label="' + escapeHtml(t('player_volume', 'Volume')) + '">' +
    '</div>' +
    '<div class="shorts-top-right-actions">' +
    '<button class="shorts-top-control-btn shorts-more-btn" type="button" data-short-top-action="more" title="' + escapeHtml(t('action_more', 'More')) + '" aria-label="' + escapeHtml(t('action_more', 'More')) + '" aria-haspopup="menu" aria-expanded="false">' + iconSvg('more') + '</button>' +
    '<button class="shorts-top-control-btn shorts-fullscreen-btn" type="button" data-short-top-action="fullscreen" title="' + escapeHtml(t('action_enter_fullscreen', 'Enter fullscreen')) + '" aria-label="' + escapeHtml(t('action_enter_fullscreen', 'Enter fullscreen')) + '">' + iconSvg('fullscreen') + '</button>' +
    '</div>')
  topControls.insertBefore(header, topControls.lastElementChild)

  var storyChrome = null
  if (_state.storyMode) {
    storyChrome = doc.createElement('div')
    storyChrome.className = 'shorts-story-chrome hidden'
    storyChrome.setAttribute('data-story-chrome', '')
    safeSetMarkup(storyChrome, '' +
      '<div class="shorts-story-progress"></div>'
    )
    wrapper.appendChild(storyChrome)
  }

  var actions = doc.createElement('div')
  actions.className = 'shorts-actions'
  var avatarMarkup = entryData.avatarUrl
    ? ('<img class="channel-avatar-img" src="' + escapeHtml(entryData.avatarUrl) + '" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer" data-avatar-fallback="' + channelInitial + '">')
    : ('<span class="shorts-channel-avatar-fallback">' + channelInitial + '</span>')
  var followBadge = ''
  if (entryData.channelId && (!entryData.liveRoomId || !entryData.channelFollowed)) {
    followBadge = '<button class="shorts-rail-follow-badge' + (entryData.channelFollowed ? ' is-following' : '') + '" type="button" data-short-follow="1" data-channel-id="' + escapeHtml(entryData.channelId) + '" data-following="' + (entryData.channelFollowed ? '1' : '0') + '" title="' + escapeHtml(entryData.channelFollowed ? t('action_following', 'Following') : t('action_follow', 'Follow')) + '" aria-label="' + escapeHtml(entryData.channelFollowed ? t('action_following', 'Following') : t('action_follow', 'Follow')) + '">' + iconSvg(entryData.channelFollowed ? 'check' : 'add') + '</button>'
  }
  var storyState = normalizeStoryState(entryData.storyState)
  var storyAttrs = (!_state.storyMode && (storyState !== 'none' || entryData.liveRoomId) && entryData.channelId)
    ? (' data-story-channel-id="' + escapeHtml(entryData.channelId) + '" data-story-first-video-id="' + escapeHtml(entryData.liveRoomId ? 'tiktok_live_' + entryData.liveRoomId : entryData.storyFirstVideoId || '') + '" data-story-state="' + escapeHtml(storyState) + '"')
    : ''
  var avatarLinkClass = 'shorts-rail-avatar-link story-ring-' + (entryData.liveRoomId ? 'live' : storyState)
  var actionsHtml = '' +
    '<div class="shorts-rail-avatar-wrap">' +
    '<a class="' + avatarLinkClass + '" href="' + escapeHtml(channelHref) + '"' +
    (entryData.channelId ? (' data-channel-id="' + escapeHtml(entryData.channelId) + '"') : '') + storyAttrs + '>' +
    '<span class="shorts-rail-avatar" aria-hidden="true">' + avatarMarkup + '</span>' +
    (entryData.liveRoomId ? '<span class="shorts-live-badge">LIVE</span>' : '') +
    '</a>' +
    followBadge +
    '</div>' +
    '<button class="action-btn shorts-autoplay-btn" type="button" data-short-action="autoplay" title="' + escapeHtml(t('shorts_autoplay_next', 'Auto-play next short')) + '">' + iconSvg('autoplay', false) + '</button>' +
    '<button class="action-btn bookmark-btn shorts-bookmark-btn" type="button" data-short-action="bookmark" title="' + escapeHtml(t('action_bookmark', 'Bookmark')) + '">' + iconSvg('bookmark', !!entryData.bookmarked) + '</button>' +
    '<button class="action-btn shorts-share-btn" type="button" data-short-action="share" title="' + escapeHtml(t('action_share', 'Share')) + '">' + iconSvg('share', false) + '</button>' +
    ((video || slideshow) ? '<button class="action-btn shorts-mini-player-btn" type="button" data-short-action="mini-player" title="' + escapeHtml(t('mini_player_title', 'Mini player')) + '" aria-label="' + escapeHtml(t('mini_player_title', 'Mini player')) + '">' + menuIconSvg('mini') + '</button>' : '')
  safeSetMarkup(actions, actionsHtml)

  var info = doc.createElement('div')
  info.className = 'shorts-info-overlay'
  var ts = doc.createElement('div')
  ts.className = 'shorts-timestamp'
  ts.textContent = timeLabel || ''
  var publishedAt = parseAppDate(entryData.publishedAt)
  if (publishedAt) ts.setAttribute('data-timestamp', String(publishedAt.getTime()))
  var repost = makeRepostLabel(entryData)
  var title = doc.createElement('div')
  title.className = 'shorts-video-title'
  var rawTitle = String(entryData.title || '').trim()
  var rawDesc = String(entryData.description || '').trim()
  var placeholderShortTitle = /^x\s+post\s+['"]?\d+['"]?$/i.test(rawTitle)
  var displayText
  if (placeholderShortTitle) {
    displayText = rawDesc
  } else if (rawDesc && (rawTitle.endsWith('...') || rawDesc.length > rawTitle.length + 10)) {
    displayText = rawDesc
  } else {
    displayText = rawTitle || rawDesc
  }
  safeSetMarkup(title, linkifyTitleHandles(displayText || '', entryData.platform))
  title.addEventListener('click', function (e) {
    if (e.target && e.target.closest && e.target.closest('a')) {
      e.stopPropagation()
      return
    }
    e.preventDefault()
    e.stopPropagation()
    var expanded = title.classList.toggle('expanded')
    if (desc) desc.classList.toggle('expanded', expanded)
  })
  if (slideshow && slideshow.count > 1) {
    var slideControls = doc.createElement('div')
    slideControls.className = 'shorts-slide-controls'

    var prevSlideBtn = doc.createElement('button')
    prevSlideBtn.className = 'slide-arrow prev'
    prevSlideBtn.type = 'button'
    prevSlideBtn.setAttribute('aria-label', t('action_previous_slide', 'Previous slide'))
    safeSetMarkup(prevSlideBtn, iconSvg('prev'))
    prevSlideBtn.addEventListener('click', function (e) {
      e.preventDefault()
      e.stopPropagation()
      stepSlideshow({ refs: { slideshow: slideshow } }, -1)
    })

    var dotsEl = doc.createElement('div')
    dotsEl.className = 'slide-dots'
    for (let di = 0; di < slideshow.count; di += 1) {
      const dot = doc.createElement('button')
      dot.className = 'slide-dot' + (di === 0 ? ' active' : '')
      dot.type = 'button'
      dot.setAttribute('aria-label', tf('content_description_slide_number', 'Slide %1$d', di + 1))
      dot.setAttribute('aria-current', di === 0 ? 'true' : 'false')
      dot.addEventListener('click', function (e) {
        e.preventDefault()
        e.stopPropagation()
        goToSlideshowSlide({ refs: { slideshow: slideshow } }, di)
      })
      dotsEl.appendChild(dot)
      slideshow.dots.push(dot)
    }

    var nextSlideBtn = doc.createElement('button')
    nextSlideBtn.className = 'slide-arrow next'
    nextSlideBtn.type = 'button'
    nextSlideBtn.setAttribute('aria-label', t('action_next_slide', 'Next slide'))
    safeSetMarkup(nextSlideBtn, iconSvg('next'))
    nextSlideBtn.addEventListener('click', function (e) {
      e.preventDefault()
      e.stopPropagation()
      stepSlideshow({ refs: { slideshow: slideshow } }, 1)
    })

    slideControls.appendChild(prevSlideBtn)
    slideControls.appendChild(dotsEl)
    slideControls.appendChild(nextSlideBtn)
    info.appendChild(slideControls)
  }
  if (repost) info.appendChild(repost)
  info.appendChild(ts)
  var author = entryData.channelId ? doc.createElement('a') : doc.createElement('div')
  author.className = 'shorts-author-name'
  author.textContent = entryData.channelName || t('common_unknown', 'Unknown')
  if (entryData.channelId) {
    author.classList.add('shorts-channel')
    author.href = channelHref
    author.setAttribute('data-channel-id', entryData.channelId)
    author.addEventListener('click', function (e) {
      e.stopPropagation()
    })
  }
  info.appendChild(author)
  info.appendChild(title)
  var desc = null
  var descToggle = null
  wrapper.appendChild(info)

  var progressContainer = doc.createElement('div')
  progressContainer.className = 'val-progress-container'
  var progressBar = doc.createElement('div')
  progressBar.className = 'val-progress-bar'
  progressContainer.appendChild(progressBar)
  if (isLive || slideshow && slideshow.count > 0) {
    progressContainer.style.display = 'none'
  }
  wrapper.appendChild(progressContainer)

  item.appendChild(wrapper)
  item.appendChild(actions)
  item.appendChild(topControls)

  var refs = {
    video: video,
    poster: poster,
    wrapper: wrapper,
    mediaStage: mediaStage,
    actions: actions,
    info: info,
    author: author,
    muteBtn: q('.shorts-mute-btn', topControls),
    volumeSlider: q('.shorts-volume-slider', topControls),
    moreBtn: q('.shorts-more-btn', topControls),
    fullscreenBtn: q('.shorts-fullscreen-btn', topControls),
    miniPlayerBtn: q('.shorts-mini-player-btn', actions),
    autoplayBtn: q('.shorts-autoplay-btn', actions),
    bookmarkBtn: q('.shorts-bookmark-btn', actions),
    shareBtn: q('.shorts-share-btn', actions),
    slideshow: slideshow,
    progressContainer: progressContainer,
    progressBar: progressBar,
    storyChrome: storyChrome,
    title: title,
    desc: desc,
    descToggle: descToggle
  }
  var entryObj = { el: item, data: entryData, refs: refs }
  if (isLive) initTikTokLive(entryObj)
  refs.disposeActions = function () { if (momentActionsWrapper === wrapper) closeMomentActions() }
  wrapper.addEventListener('contextmenu', function (event) {
    if (event.target.closest('a, input, .moment-actions-overlay')) return
    event.preventDefault()
    event.stopPropagation()
    openMomentActions(entryObj, refs.moreBtn, { x: event.clientX, y: event.clientY })
  })

  if (video) {
    function revealVideoFrame() {
      wrapper.classList.remove('is-awaiting-first-frame')
    }
    video.addEventListener('loadedmetadata', function () {
      maybeMarkAspect(wrapper, video)
    })
    video.addEventListener('loadeddata', revealVideoFrame)
    video.addEventListener('canplay', revealVideoFrame)
    video.addEventListener('playing', revealVideoFrame)
    video.addEventListener('playing', function () { if (!isLive) setMomentSubtitles(entryObj) })
    video.addEventListener('timeupdate', function () {
      handleVideoTimeUpdate({ refs: refs })
    })
    video.addEventListener('durationchange', function () { handleVideoTimeUpdate(entryObj) })
    video.addEventListener('progress', function () { handleVideoTimeUpdate(entryObj) })
    video.addEventListener('emptied', function () { handleVideoTimeUpdate(entryObj) })
    makeDraggableSeekbar(progressContainer, progressBar, video)
    attachSeekTooltip(progressContainer, video)
    video.loop = !isLive && !autoAdvanceEnabled()
    video.muted = _state.muted
    video.volume = _state.volume
    video.playbackRate = _state.playbackRate

    var miniControls = createFeedVideoControls({ cinema: false, autoplay: true })
    miniControls.classList.add('shorts-mini-controls')
    mediaStage.appendChild(miniControls)
    refs.miniControls = miniControls
    refs.disposeVideoControls = bindFeedVideoControls(mediaStage, video, {
      cinema: false,
      autoplay: true,
      volumeKey: 'shortsVolume',
      getAutoplay: function () {
        return autoAdvanceEnabled()
      },
      onAutoplayToggle: function () {
        _state.autoPlayNext = !_state.autoPlayNext
        try { localStorage.setItem('shortsAutoPlayNext', _state.autoPlayNext) } catch (_) {}
        _state.items.forEach(function (e) {
          var a = e && e.refs && e.refs.slideshow && e.refs.slideshow.audio
          if (a) a.loop = !_state.autoPlayNext
        })
        syncRenderedShortVideoLoop()
        if (_fns && typeof _fns.updateCurrentActionButtons === 'function') {
          _fns.updateCurrentActionButtons()
        }
        showToast(tf('shorts_autoplay_next_state', 'Auto-play next short: %1', _state.autoPlayNext ? t('state_on', 'ON') : t('state_off', 'OFF')))
      },
      onVolumeChange: function (vol, muted) {
        _state.volume = vol
        _state.muted = muted
        try {
          localStorage.setItem('shortsVolume', vol)
          localStorage.setItem('shortsMuted', muted)
        } catch (_) {}
        applyShortMediaPreferences()
        if (_fns && typeof _fns.updateTopControls === 'function') {
          _fns.updateTopControls()
        }
      },
      onRateChange: function (rate) {
        _state.playbackRate = rate
        try { localStorage.setItem('shortsPlaybackRate', rate) } catch (_) {}
        applyShortMediaPreferences()
      },
      onFullscreen: function () {
        toggleMomentFullscreen(entryObj)
      },
      onMini: function () {
        toggleMomentMiniPlayer(entryObj)
      },
    })
    video.addEventListener('ended', function () {
      if (isLive) return
      if (autoAdvanceEnabled()) _fns.goNext()
      else {
        try {
          video.currentTime = 0
          video.play().catch(function () {})
        } catch (_) { }
      }
    })
    video.addEventListener('click', function (e) {
      if (navigateStoryFromClick(entryObj, e)) return
      e.preventDefault()
      e.stopPropagation()
      toggleShortPlayback(entryObj)
    })
    video.addEventListener('error', function () {
      revealVideoFrame()
      if (isLive) return
      wrapper.classList.add('shorts-video-error')
      var cur = _fns && typeof _fns.currentData === 'function' ? _fns.currentData() : null
      if (cur && entryData.id === cur.id) {
        showToast(t('shorts_media_unavailable_skipping', 'Short media unavailable, skipping'))
        if (_fns && typeof _fns.goNext === 'function') {
          setTimeout(_fns.goNext, 120)
        }
      }
    })
    attachShortVideoDebug(entryObj)
  } else if (slideshow && slideshow.slides && slideshow.slides.length) {
    var firstSlide = slideshow.slides[0]
    if (firstSlide) {
      firstSlide.addEventListener('error', function () {
        wrapper.classList.add('shorts-video-error')
        var cur = _fns && typeof _fns.currentData === 'function' ? _fns.currentData() : null
        if (cur && entryData.id === cur.id) {
          showToast(t('shorts_media_unavailable_skipping', 'Short media unavailable, skipping'))
          if (_fns && typeof _fns.goNext === 'function') {
            setTimeout(_fns.goNext, 120)
          }
        }
      }, { once: true })
    }
  }
  var avatarImg = q('.channel-avatar-img', item)
  if (avatarImg) {
    avatarImg.addEventListener('error', function () {
      var fb = escapeHtml(String(avatarImg.getAttribute('data-avatar-fallback') || 'U'))
      var holder = avatarImg.parentNode
      if (!holder) return
      var fallback = doc.createElement('span')
      fallback.className = 'shorts-channel-avatar-fallback'
      fallback.textContent = fb
      holder.replaceChildren(fallback)
    }, { once: true })
  }
  var repostAvatarImg = q('.shorts-repost-avatar-img', wrapper)
  if (repostAvatarImg) {
    repostAvatarImg.addEventListener('error', function () {
      var fb = String(repostAvatarImg.getAttribute('data-avatar-fallback') || '?')
      var fallback = doc.createElement('span')
      fallback.className = 'shorts-repost-avatar-fallback'
      fallback.textContent = fb
      repostAvatarImg.replaceWith(fallback)
    }, { once: true })
  }

  progressContainer.addEventListener('click', function (e) {
    if (!video) return
    e.preventDefault()
    e.stopPropagation()
    var range = playbackRange(video)
    if (!range) return
    var rect = progressContainer.getBoundingClientRect()
    if (!(rect.width > 0)) return
    var x = Math.max(0, Math.min(rect.width, e.clientX - rect.left))
    var pct = x / rect.width
    video.currentTime = range.start + pct * (range.end - range.start)
  })

  actions.addEventListener('click', function (e) {
    var storyAvatar = e.target && e.target.closest ? e.target.closest('.shorts-rail-avatar-link[data-story-channel-id]') : null
    if (storyAvatar && _fns.openStoryChannel) {
      e.preventDefault()
      e.stopPropagation()
      _fns.openStoryChannel(
        storyAvatar.getAttribute('data-story-channel-id'),
        storyAvatar.getAttribute('data-story-first-video-id')
      )
      return
    }
    var followBtn = e.target && e.target.closest ? e.target.closest('[data-short-follow]') : null
    if (followBtn) {
      e.preventDefault()
      e.stopPropagation()
      followShortAuthor(entryObj, followBtn)
      return
    }
    var btn = e.target && e.target.closest ? e.target.closest('[data-short-action]') : null
    if (!btn) return
    e.preventDefault()
    e.stopPropagation()
    var action = btn.getAttribute('data-short-action')
    if (action === 'autoplay') {
      if (_state.storyMode) return
      _state.autoPlayNext = !_state.autoPlayNext
      localStorage.setItem('shortsAutoPlayNext', _state.autoPlayNext)
      syncRenderedShortVideoLoop()
      _state.items.forEach(function (e) {
        var a = e && e.refs && e.refs.slideshow && e.refs.slideshow.audio
        if (a) a.loop = !_state.autoPlayNext
      })
      _fns.updateCurrentActionButtons()
      showToast(t('shorts_autoplay_next_state', 'Auto-play next short: %1$s')
        .replace('%1$s', _state.autoPlayNext ? t('state_on', 'ON') : t('state_off', 'OFF')))
      return
    }
    if (action === 'mini-player') {
      toggleMomentMiniPlayer(entryObj)
      return
    }
    if (action === 'share') {
      shareShort(entryData, btn)
      return
    }
    if (action === 'bookmark') {
      handleBookmarkAction(entryData, btn)
      return
    }
  })

  topControls.addEventListener('click', function (e) {
    var btn = e.target && e.target.closest ? e.target.closest('[data-short-top-action]') : null
    if (!btn) return
    e.preventDefault()
    e.stopPropagation()
    var action = btn.getAttribute('data-short-top-action')
    if (action === 'mute') toggleShortMute()
    else if (action === 'more') {
      if (momentActionsWrapper === wrapper) closeMomentActions()
      else openMomentActions(entryObj, btn)
    }
    else if (action === 'fullscreen') toggleMomentFullscreen(entryObj)
  })
  bindVolumeWheel(topControls.querySelector('.shorts-volume-control'), function () {
    return _state.muted ? 0 : _state.volume
  }, setShortVolume)
  refs.volumeSlider.addEventListener('input', function (e) {
    e.stopPropagation()
    setShortVolume(refs.volumeSlider.value)
  })
  refs.volumeSlider.addEventListener('click', function (e) { e.stopPropagation() })
  refs.volumeSlider.addEventListener('pointerdown', function (e) { e.stopPropagation() })
  refs.volumeSlider.addEventListener('pointerup', function () { refs.volumeSlider.blur() })
  refs.volumeSlider.addEventListener('pointercancel', function () { refs.volumeSlider.blur() })

  wrapper.addEventListener('click', function (e) {
    var clickOnControl = e.target && e.target.closest && e.target.closest('.shorts-actions, .shorts-player-controls, .shorts-header-overlay, .shorts-story-chrome, .val-progress-container, .shorts-slide-controls, .moment-actions-overlay, .shorts-live-chat')
    if (clickOnControl) return
    if (navigateStoryFromClick(entryObj, e)) return
    toggleShortPlayback(entryObj)
  })

  return entryObj
}

function syncShortAuthorFollow(channelId, following) {
  var cid = String(channelId || '').trim()
  if (!cid) return
  if (_state && _state.items) {
    _state.items.forEach(function (entry) {
      if (entry && entry.data && entry.data.channelId === cid) {
        entry.data.channelFollowed = !!following
      }
    })
  }
  document.querySelectorAll('[data-channel-id="' + cssEscape(cid) + '"]').forEach(function (el) {
    el.setAttribute('data-channel-followed', following ? '1' : '0')
  })
  document.querySelectorAll('[data-short-follow][data-channel-id="' + cssEscape(cid) + '"]').forEach(function (el) {
    el.setAttribute('data-following', following ? '1' : '0')
    el.classList.toggle('is-following', !!following)
    el.setAttribute('title', following ? t('action_following', 'Following') : t('action_follow', 'Follow'))
    el.setAttribute('aria-label', following ? t('action_following', 'Following') : t('action_follow', 'Follow'))
    el.disabled = false
    safeSetMarkup(el, iconSvg(following ? 'check' : 'add'))
  })
  document.querySelectorAll('[data-feed-follow-toggle][data-feed-channel-id="' + cssEscape(cid) + '"]').forEach(function (el) {
    el.setAttribute('data-following', following ? '1' : '0')
    el.classList.toggle('following', !!following)
    el.textContent = following ? t('action_following', 'Following') : t('action_follow', 'Follow')
  })
  if (window.MpaSiteBase && typeof window.MpaSiteBase.syncChannelFollowState === 'function') {
    window.MpaSiteBase.syncChannelFollowState(cid, following)
  }
}

function followShortAuthor(entry, btn) {
  var entryData = entry && entry.data
  if (!entryData || !entryData.channelId || !btn || btn.disabled) return
  var channelId = String(entryData.channelId || '').trim()
  var handle = channelId.replace(/^(tiktok|instagram|youtube|twitter|x)_/, '')
  var label = entryData.channelName || handle || channelId
  var following = btn.getAttribute('data-following') === '1' || !!entryData.channelFollowed
  btn.disabled = true
  var op
  if (following) {
    op = askConfirm({
      title: t('confirm_unfollow_channel_title', 'Unfollow Channel'),
      body: tf('confirm_unfollow_channel_body', 'Unfollow %1$s?', label),
      confirmLabel: t('action_unfollow', 'Unfollow'),
      cancelLabel: t('action_cancel', 'Cancel'),
      danger: true
    }).then(function (confirmed) {
      if (!confirmed) return null
      syncShortAuthorFollow(channelId, false)
      return apiFetch('/api/mutations/follow', {
        method: 'POST',
        body: JSON.stringify({ channel_id: channelId, action: 'clear', updated_at_ms: Date.now() })
      })
    }).then(function (payload) {
      if (!payload) return false
      finishMomentUnfollow(entry, channelId, label, payload && payload.message)
      return true
    })
  } else {
    syncShortAuthorFollow(channelId, true)
    op = apiFetch('/api/mutations/follow', {
      method: 'POST',
      body: JSON.stringify({ channel_id: channelId, action: 'set', updated_at_ms: Date.now() })
    }).then(function () {
      showToast(tf('toast_followed_channel', 'Followed %1$s', label))
      return true
    })
  }
  op.catch(function (err) {
    if (following) syncShortAuthorFollow(channelId, true)
    else syncShortAuthorFollow(channelId, false)
    showToast((err && err.payload && err.payload.error) ? err.payload.error : (following ? t('error_unfollow_failed', 'Failed to unfollow') : t('error_follow_failed', 'Failed to follow')))
  }).finally(function () {
    btn.disabled = false
  })
}
