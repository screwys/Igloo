// Shorts items — DOM builder, action button handlers, card parsing.

import { apiFetch, askConfirm, cssEscape, escapeHtml, showToast, copyText, makeDraggableSeekbar, attachSeekTooltip, formatRelative, t, tf, toFxTwitterUrl } from '../utils.js'
import { openBookmarkMenu } from '../bookmark-menu.js'
import { maybeMarkAspect, handleVideoTimeUpdate, toggleShortPlayback, setSlideshowIndex, stepSlideshow, syncRenderedShortVideoLoop } from './playback.js'
import { attachShortVideoDebug } from './debug.js'

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

function closeMomentActions() {
  if (momentActionsSheet && momentActionsSheet.parentNode) momentActionsSheet.remove()
  momentActionsSheet = null
  if (momentActionsWrapper) momentActionsWrapper.classList.remove('moment-actions-open')
  momentActionsWrapper = null
  if (momentActionsTrigger) momentActionsTrigger.setAttribute('aria-expanded', 'false')
  momentActionsTrigger = null
  if (momentActionsKeyHandler) document.removeEventListener('keydown', momentActionsKeyHandler)
  momentActionsKeyHandler = null
  if (momentActionsOutsideHandler) document.removeEventListener('pointerdown', momentActionsOutsideHandler, true)
  momentActionsOutsideHandler = null
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
  var volume = Math.max(0, Math.min(1, Number(_state.volume)))
  var rate = Number(_state.playbackRate) > 0 ? Number(_state.playbackRate) : 1
  document.querySelectorAll('#shorts-container video, #shorts-container audio').forEach(function (media) {
    media.volume = volume
    media.muted = !!_state.muted
    media.playbackRate = rate
  })
}

function setShortVolume(value) {
  var volume = Math.max(0, Math.min(1, Number(value)))
  _state.volume = Number.isFinite(volume) ? volume : 1
  _state.muted = _state.volume === 0
  localStorage.setItem('shortsVolume', String(_state.volume))
  localStorage.setItem('shortsMuted', String(_state.muted))
  applyShortMediaPreferences()
  _fns.updateCurrentActionButtons()
}

function toggleShortMute() {
  _state.muted = !_state.muted
  if (!_state.muted && !(_state.volume > 0)) _state.volume = 1
  localStorage.setItem('shortsVolume', String(_state.volume))
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

function toggleMomentMiniPlayer(entry) {
  var refs = entry && entry.refs
  if (!refs || !refs.wrapper || !refs.video) return false
  var manager = null
  try { manager = window.top && window.top.IglooMiniPlayer } catch (_) {}
  if (!manager || typeof manager.toggleSurface !== 'function') return false
  return manager.toggleSurface({
    element: refs.wrapper,
    video: refs.video,
    button: refs.miniPlayerBtn,
    title: String(entry.data && (entry.data.channelName || entry.data.title) || '').trim() || t('mini_player_title', 'Mini player'),
    kind: 'moments',
    homeURL: window.location.pathname + window.location.search,
  }) !== false
}

function shortShareUrl(entryData) {
  var shareUrl = String(entryData.originalUrl || '').trim()
  var platform = String(entryData.platform || '').trim().toLowerCase()

  if (!shareUrl) {
    shareUrl = window.location.origin + '/shorts?video=' + encodeURIComponent(entryData.id)
    if (platform === 'tiktok') {
      var handle = String(entryData.channelName || entryData.channelId || '').trim()
      var cleanHandle = handle ? (handle.startsWith('@') ? handle : ('@' + handle)) : '@user'
      shareUrl = 'https://www.tiktok.com/' + cleanHandle + '/video/' + encodeURIComponent(entryData.id)
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

function openMomentActions(entry, trigger) {
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
  safeSetMarkup(speedLabel, '<span class="moment-actions-item-icon">' + menuIconSvg('speed') + '</span><span>' + escapeHtml(t('player_playback_speed', 'Playback speed')) + '</span>')
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
  if (isRepost) {
    actions.push({ key: 'disable_reposts', icon: 'repost', label: tf('action_turn_off_reposts_for_account', 'Turn off reposts for %1$s', reposterLabel) })
  }
  if (entry.refs && entry.refs.video) {
    actions.push({ key: 'mini_player', icon: 'mini', label: t('mini_player_title', 'Mini player') })
  }
  actions.push({ key: 'share', icon: 'share', label: t('action_share', 'Share') })
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
    button.setAttribute('role', 'menuitem')
    safeSetMarkup(button, '<span class="moment-actions-item-icon">' + menuIconSvg(action.icon) + '</span><span>' + escapeHtml(action.label) + '</span>')
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
    closeMomentActions()
  }
  document.addEventListener('keydown', momentActionsKeyHandler)
  document.addEventListener('pointerdown', momentActionsOutsideHandler, true)
  wrapper.classList.add('moment-actions-open')
  wrapper.appendChild(overlay)
  momentActionsSheet = overlay
  momentActionsWrapper = wrapper
  momentActionsTrigger = trigger || null
  if (momentActionsTrigger) momentActionsTrigger.setAttribute('aria-expanded', 'true')
  requestAnimationFrame(function () { overlay.classList.add('visible') })
  return true
}

function lucideMenuIcon(paths) {
  return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + paths + '</svg>'
}

// Menu glyphs use Lucide's published 24px icon data (ISC license).
function menuIconSvg(kind) {
  if (kind === 'speed') {
    return lucideMenuIcon('<path d="m12 14 4-4"></path><path d="M3.34 19a10 10 0 1 1 17.32 0"></path>')
  }
  if (kind === 'mini') {
    return lucideMenuIcon('<path d="M2 10h6V4"></path><path d="m2 4 6 6"></path><path d="M21 10V7a2 2 0 0 0-2-2h-7"></path><path d="M3 14v2a2 2 0 0 0 2 2h3"></path><rect x="12" y="14" width="10" height="7" rx="1"></rect>')
  }
  if (kind === 'share') {
    return lucideMenuIcon('<circle cx="18" cy="5" r="3"></circle><circle cx="6" cy="12" r="3"></circle><circle cx="18" cy="19" r="3"></circle><line x1="8.59" x2="15.42" y1="13.51" y2="17.49"></line><line x1="15.41" x2="8.59" y1="6.51" y2="10.49"></line>')
  }
  if (kind === 'profile') {
    return lucideMenuIcon('<circle cx="12" cy="8" r="5"></circle><path d="M20 21a8 8 0 0 0-16 0"></path>')
  }
  if (kind === 'repost') {
    return lucideMenuIcon('<path d="m2 9 3-3 3 3"></path><path d="M13 18H7a2 2 0 0 1-2-2V6"></path><path d="m22 15-3 3-3-3"></path><path d="M11 6h6a2 2 0 0 1 2 2v10"></path>')
  }
  if (kind === 'mute-account') {
    return lucideMenuIcon('<path d="M11 4.702a.7.7 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.7.7 0 0 0 11 19.298z"></path><path d="m16.5 14.5 5-5"></path><path d="m16.5 9.5 5 5"></path>')
  }
  if (kind === 'follow') {
    return lucideMenuIcon('<path d="M2 21a8 8 0 0 1 13.292-6"></path><circle cx="10" cy="8" r="5"></circle><path d="M19 16v6"></path><path d="M22 19h-6"></path>')
  }
  if (kind === 'unfollow') {
    return lucideMenuIcon('<path d="M2 21a8 8 0 0 1 13.292-6"></path><circle cx="10" cy="8" r="5"></circle><path d="M22 19h-6"></path>')
  }
  return ''
}

export function iconSvg(kind, active) {
  if (kind === 'menu') {
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="3" y1="6" x2="21" y2="6"></line><line x1="3" y1="12" x2="21" y2="12"></line><line x1="3" y1="18" x2="21" y2="18"></line></svg>'
  }
  if (kind === 'more') {
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.8"></circle><circle cx="12" cy="12" r="1.8"></circle><circle cx="19" cy="12" r="1.8"></circle></svg>'
  }
  if (kind === 'fullscreen') {
    return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M8 3H3v5M16 3h5v5M21 16v5h-5M3 16v5h5"></path></svg>'
  }
  if (kind === 'grid') {
    return '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="6" height="6" rx="1.2"></rect><rect x="14" y="4" width="6" height="6" rx="1.2"></rect><rect x="4" y="14" width="6" height="6" rx="1.2"></rect><rect x="14" y="14" width="6" height="6" rx="1.2"></rect></svg>'
  }
  if (kind === 'tray-right') {
    return '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"></rect><path d="M15 4v16"></path><path d="M9 9l3 3-3 3"></path></svg>'
  }
  if (kind === 'prev') {
    return '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"></polyline></svg>'
  }
  if (kind === 'next') {
    return '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"></polyline></svg>'
  }
  if (kind === 'open') {
    return '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3h7v7"></path><path d="M10 14L21 3"></path><path d="M21 14v6a1 1 0 0 1-1 1h-6"></path><path d="M10 3H4a1 1 0 0 0-1 1v6"></path><path d="M3 10v10a1 1 0 0 0 1 1h10"></path></svg>'
  }
  if (kind === 'check') {
    return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"></polyline></svg>'
  }
  if (kind === 'add') {
    return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>'
  }
  if (kind === 'share') {
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8"/><polyline points="16 6 12 2 8 6"/><line x1="12" y1="2" x2="12" y2="15"/></svg>'
  }
  if (kind === 'bookmark') {
    if (active) {
      return '<svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/></svg>'
    }
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/></svg>'
  }
  if (kind === 'comment') {
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>'
  }
  if (kind === 'autoplay') {
    if (active) {
      return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10" fill="none"></circle><polygon points="10 8 16 12 10 16" fill="currentColor" stroke="none"></polygon></svg>'
    }
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><polygon points="10 8 16 12 10 16" fill="currentColor" stroke="none"></polygon></svg>'
  }
  if (kind === 'mute') {
    if (active) {
      return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"></polygon><line x1="23" y1="9" x2="17" y2="15"></line><line x1="17" y1="9" x2="23" y2="15"></line></svg>'
    }
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"></polygon><path d="M19.07 4.93a10 10 0 0 1 0 14.14"></path><path d="M15.54 8.46a5 5 0 0 1 0 7.07"></path></svg>'
  }
  if (kind === 'pause') {
    return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="5" width="4" height="14" fill="currentColor" stroke="none"></rect><rect x="14" y="5" width="4" height="14" fill="currentColor" stroke="none"></rect></svg>'
  }
  return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle></svg>'
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
    chevron.textContent = '›'
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
  } else {
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
    if (entryData.streamUrl) video.src = entryData.streamUrl
    mediaStage.appendChild(video)
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
  wrapper.appendChild(header)

  var topControls = doc.createElement('div')
  topControls.className = 'shorts-player-controls'
  var volumeValue = _state.muted ? 0 : _state.volume
  safeSetMarkup(topControls, '' +
    '<div class="shorts-volume-control">' +
    '<button class="shorts-top-control-btn shorts-mute-btn" type="button" data-short-top-action="mute" title="' + escapeHtml(_state.muted ? t('action_unmute', 'Unmute') : t('action_mute', 'Mute')) + '" aria-label="' + escapeHtml(_state.muted ? t('action_unmute', 'Unmute') : t('action_mute', 'Mute')) + '">' + iconSvg('mute', _state.muted) + '</button>' +
    '<input class="shorts-volume-slider" type="range" min="0" max="1" step="0.05" value="' + escapeHtml(String(volumeValue)) + '" aria-label="' + escapeHtml(t('player_volume', 'Volume')) + '">' +
    '</div>' +
    '<div class="shorts-top-right-actions">' +
    '<button class="shorts-top-control-btn shorts-more-btn" type="button" data-short-top-action="more" title="' + escapeHtml(t('action_more', 'More')) + '" aria-label="' + escapeHtml(t('action_more', 'More')) + '" aria-haspopup="menu" aria-expanded="false">' + iconSvg('more') + '</button>' +
    '<button class="shorts-top-control-btn shorts-fullscreen-btn" type="button" data-short-top-action="fullscreen" title="' + escapeHtml(t('action_enter_fullscreen', 'Enter fullscreen')) + '" aria-label="' + escapeHtml(t('action_enter_fullscreen', 'Enter fullscreen')) + '">' + iconSvg('fullscreen') + '</button>' +
    '</div>')
  wrapper.appendChild(topControls)

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
  if (entryData.channelId) {
    followBadge = '<button class="shorts-rail-follow-badge' + (entryData.channelFollowed ? ' is-following' : '') + '" type="button" data-short-follow="1" data-channel-id="' + escapeHtml(entryData.channelId) + '" data-following="' + (entryData.channelFollowed ? '1' : '0') + '" title="' + escapeHtml(entryData.channelFollowed ? t('action_following', 'Following') : t('action_follow', 'Follow')) + '" aria-label="' + escapeHtml(entryData.channelFollowed ? t('action_following', 'Following') : t('action_follow', 'Follow')) + '">' + iconSvg(entryData.channelFollowed ? 'check' : 'add') + '</button>'
  }
  var storyState = normalizeStoryState(entryData.storyState)
  var storyAttrs = (!_state.storyMode && storyState !== 'none' && entryData.channelId)
    ? (' data-story-channel-id="' + escapeHtml(entryData.channelId) + '" data-story-first-video-id="' + escapeHtml(entryData.storyFirstVideoId || '') + '" data-story-state="' + escapeHtml(storyState) + '"')
    : ''
  var avatarLinkClass = 'shorts-rail-avatar-link story-ring-' + storyState
  var externalLabel = t('action_open_externally', 'Open externally')
  var externalAction = entryData.originalUrl
    ? '<a class="action-btn shorts-external-btn" href="' + escapeHtml(entryData.originalUrl) + '" target="_blank" rel="noopener noreferrer" title="' + escapeHtml(externalLabel) + '" aria-label="' + escapeHtml(externalLabel) + '">' + iconSvg('open') + '</a>'
    : ''
  var actionsHtml = '' +
    '<div class="shorts-rail-avatar-wrap">' +
    '<a class="' + avatarLinkClass + '" href="' + escapeHtml(channelHref) + '"' +
    (entryData.channelId ? (' data-channel-id="' + escapeHtml(entryData.channelId) + '"') : '') + storyAttrs + '>' +
    '<span class="shorts-rail-avatar" aria-hidden="true">' + avatarMarkup + '</span>' +
    '</a>' +
    followBadge +
    '</div>' +
    '<button class="action-btn shorts-autoplay-btn" type="button" data-short-action="autoplay" title="' + escapeHtml(t('shorts_autoplay_next', 'Auto-play next short')) + '">' + iconSvg('autoplay', false) + '</button>' +
    '<button class="action-btn bookmark-btn shorts-bookmark-btn" type="button" data-short-action="bookmark" title="' + escapeHtml(t('action_bookmark', 'Bookmark')) + '">' + iconSvg('bookmark', !!entryData.bookmarked) + '</button>' +
    '<button class="action-btn shorts-share-btn" type="button" data-short-action="share" title="' + escapeHtml(t('action_share', 'Share')) + '">' + iconSvg('share', false) + '</button>' +
    (video ? '<button class="action-btn shorts-mini-player-btn" type="button" data-short-action="mini-player" title="' + escapeHtml(t('mini_player_title', 'Mini player')) + '" aria-label="' + escapeHtml(t('mini_player_title', 'Mini player')) + '">' + menuIconSvg('mini') + '</button>' : '') +
    externalAction
  safeSetMarkup(actions, actionsHtml)

  var info = doc.createElement('div')
  info.className = 'shorts-info-overlay'
  var ts = doc.createElement('div')
  ts.className = 'shorts-timestamp'
  ts.textContent = timeLabel || ''
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
    safeSetMarkup(prevSlideBtn, '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="15 18 9 12 15 6"></polyline></svg>')
    prevSlideBtn.addEventListener('click', function (e) {
      e.preventDefault()
      e.stopPropagation()
      stepSlideshow({ refs: { slideshow: slideshow } }, -1)
    })

    var dotsEl = doc.createElement('div')
    dotsEl.className = 'slide-dots'
    for (var di = 0; di < slideshow.count; di += 1) {
      var dot = doc.createElement('span')
      dot.className = 'slide-dot' + (di === 0 ? ' active' : '')
      dotsEl.appendChild(dot)
      slideshow.dots.push(dot)
    }

    var nextSlideBtn = doc.createElement('button')
    nextSlideBtn.className = 'slide-arrow next'
    nextSlideBtn.type = 'button'
    nextSlideBtn.setAttribute('aria-label', t('action_next_slide', 'Next slide'))
    safeSetMarkup(nextSlideBtn, '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="9 18 15 12 9 6"></polyline></svg>')
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
  if (slideshow && slideshow.count > 0) {
    progressContainer.style.display = 'none'
  }
  wrapper.appendChild(progressContainer)

  item.appendChild(wrapper)
  item.appendChild(actions)

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
    video.addEventListener('timeupdate', function () {
      handleVideoTimeUpdate({ refs: refs })
    })
    makeDraggableSeekbar(progressContainer, progressBar, video)
    attachSeekTooltip(progressContainer, video)
    video.loop = !autoAdvanceEnabled()
    video.muted = _state.muted
    video.volume = _state.volume
    video.playbackRate = _state.playbackRate
    video.addEventListener('ended', function () {
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
      wrapper.classList.add('shorts-video-error')
      showToast(t('shorts_media_unavailable_skipping', 'Short media unavailable, skipping'))
      var cur = _fns.currentData()
      if (cur && entryData.id === cur.id) {
        setTimeout(_fns.goNext, 120)
      }
    })
    attachShortVideoDebug(entryObj)
  } else if (slideshow && slideshow.slides && slideshow.slides.length) {
    var firstSlide = slideshow.slides[0]
    if (firstSlide) {
      firstSlide.addEventListener('error', function () {
        wrapper.classList.add('shorts-video-error')
        showToast(t('shorts_media_unavailable_skipping', 'Short media unavailable, skipping'))
        var cur = _fns.currentData()
        if (cur && entryData.id === cur.id) {
          setTimeout(_fns.goNext, 120)
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
    var dur = Number(video.duration || 0)
    if (!(dur > 0)) return
    var rect = progressContainer.getBoundingClientRect()
    if (!(rect.width > 0)) return
    var x = Math.max(0, Math.min(rect.width, e.clientX - rect.left))
    var pct = x / rect.width
    video.currentTime = pct * dur
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
  refs.volumeSlider.addEventListener('input', function (e) {
    e.stopPropagation()
    setShortVolume(refs.volumeSlider.value)
  })
  refs.volumeSlider.addEventListener('click', function (e) { e.stopPropagation() })
  refs.volumeSlider.addEventListener('pointerdown', function (e) { e.stopPropagation() })
  refs.volumeSlider.addEventListener('pointerup', function () { refs.volumeSlider.blur() })
  refs.volumeSlider.addEventListener('pointercancel', function () { refs.volumeSlider.blur() })

  wrapper.addEventListener('click', function (e) {
    var clickOnControl = e.target && e.target.closest && e.target.closest('.shorts-actions, .shorts-player-controls, .shorts-header-overlay, .shorts-story-chrome, .val-progress-container, .shorts-slide-controls, .moment-actions-overlay')
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
