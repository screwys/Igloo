import { attachSeekTooltip, makeDraggableSeekbar, materialIconMarkup, setSvgContent, t, tf } from '../utils.js'
import { bindVideoControlsVisibility } from '../video-controls-visibility.js'
import { readStoredVolume, volumeIconLevel, writeStoredVolume } from '../volume.js'

const FEED_VOLUME_KEY = 'feedVolume'

const videoControlIcons = {
  play: materialIconMarkup('PlayArrow'),
  pause: materialIconMarkup('Pause'),
  muted: materialIconMarkup('VolumeOff'),
  low: materialIconMarkup('VolumeDown'),
  high: materialIconMarkup('VolumeUp'),
  mini: materialIconMarkup('PictureInPictureAlt'),
  cinema: materialIconMarkup('VerticalSplit'),
  fullscreen: materialIconMarkup('Fullscreen'),
  autoplay: materialIconMarkup('PlayCircleOutline'),
  autoplayActive: materialIconMarkup('PlayCircle')
}

const playbackRates = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3]

function formatPlaybackRate(rate) {
  return Number(rate) + 'x'
}

function makeControlButton(name, label, icon) {
  const button = document.createElement('button')
  button.className = 'feed-video-control-btn'
  button.type = 'button'
  button.setAttribute('aria-label', label)
  button.setAttribute('data-feed-video-control', '')
  button.setAttribute(name, '')
  setSvgContent(button, videoControlIcons[icon])
  return button
}

function makeVolumeControl() {
  const control = document.createElement('div')
  control.className = 'feed-video-volume-control'
  control.setAttribute('data-feed-video-volume-control', '')

  const popover = document.createElement('div')
  popover.className = 'feed-video-volume-popover'
  const slider = document.createElement('input')
  slider.className = 'feed-video-volume-slider'
  slider.type = 'range'
  slider.min = '0'
  slider.max = '1'
  slider.step = '0.05'
  slider.value = '1'
  slider.setAttribute('orient', 'vertical')
  slider.setAttribute('aria-label', t('player_volume', 'Volume'))
  slider.setAttribute('data-feed-video-control', '')
  slider.setAttribute('data-feed-video-volume', '')
  popover.appendChild(slider)
  const thumb = document.createElement('span')
  thumb.className = 'feed-video-volume-thumb'
  thumb.setAttribute('aria-hidden', 'true')
  popover.appendChild(thumb)
  control.appendChild(popover)
  control.appendChild(makeControlButton('data-feed-video-mute', t('action_mute', 'Mute'), 'high'))
  return control
}

function makeSpeedControl() {
  const control = document.createElement('div')
  control.className = 'feed-video-speed-menu-wrap'
  control.setAttribute('data-feed-video-speed', '')

  const button = document.createElement('button')
  button.className = 'feed-video-control-btn feed-video-speed-btn'
  button.type = 'button'
  button.setAttribute('aria-label', t('player_playback_speed', 'Playback speed'))
  button.setAttribute('aria-haspopup', 'menu')
  button.setAttribute('aria-expanded', 'false')
  button.setAttribute('data-feed-video-control', '')
  button.setAttribute('data-feed-video-speed-button', '')
  button.textContent = '1x'
  control.appendChild(button)

  const menu = document.createElement('div')
  menu.className = 'feed-video-speed-menu hidden'
  menu.setAttribute('role', 'menu')
  menu.setAttribute('aria-label', t('player_playback_speed', 'Playback speed'))
  menu.setAttribute('data-feed-video-speed-menu', '')

  playbackRates.forEach(function (rate) {
    const item = document.createElement('button')
    item.className = 'feed-video-speed-option'
    if (rate === 1) item.classList.add('is-active')
    item.type = 'button'
    item.setAttribute('role', 'menuitemradio')
    item.setAttribute('aria-checked', rate === 1 ? 'true' : 'false')
    item.setAttribute('data-rate', String(rate))
    item.setAttribute('data-feed-video-control', '')
    item.textContent = formatPlaybackRate(rate)
    menu.appendChild(item)
  })

  control.appendChild(menu)
  return control
}

export function createFeedVideoControls(options) {
  const opts = options || {}
  const controls = document.createElement('div')
  controls.className = 'feed-video-controls'
  controls.setAttribute('data-feed-video-controls', '')

  controls.appendChild(makeControlButton('data-feed-video-play', t('action_play', 'Play'), 'play'))

  const progress = document.createElement('div')
  progress.className = 'feed-video-progress'
  progress.setAttribute('data-feed-progress', '')
  progress.setAttribute('role', 'slider')
  progress.setAttribute('aria-label', t('player_seek_video', 'Seek video'))
  progress.tabIndex = 0
  const fill = document.createElement('div')
  fill.className = 'feed-video-progress-fill'
  fill.setAttribute('data-feed-progress-fill', '')
  progress.appendChild(fill)
  controls.appendChild(progress)

  if (opts.autoplay) {
    const autoplayBtn = makeControlButton('data-feed-video-autoplay', t('shorts_autoplay_next', 'Auto-play next short'), 'autoplay')
    autoplayBtn.setAttribute('aria-pressed', 'false')
    controls.appendChild(autoplayBtn)
  }

  controls.appendChild(makeSpeedControl())
  controls.appendChild(makeVolumeControl())

  if (opts.mini !== false) {
    const miniButton = makeControlButton('data-feed-video-mini', t('mini_player_title', 'Mini player'), 'mini')
    miniButton.setAttribute('aria-pressed', 'false')
    controls.appendChild(miniButton)
  }

  if (opts.cinema !== false) {
    controls.appendChild(makeControlButton('data-feed-video-cinema', t('player_cinema_view', 'Cinema view'), 'cinema'))
  }

  if (opts.fullscreen !== false) {
    controls.appendChild(makeControlButton('data-feed-video-fullscreen', t('action_enter_fullscreen', 'Enter fullscreen'), 'fullscreen'))
  }

  return controls
}

function fullscreenElement(ownerDocument) {
  return ownerDocument.fullscreenElement || ownerDocument.webkitFullscreenElement || null
}

export function exitFeedVideoFullscreen(video) {
  if (!(video instanceof HTMLVideoElement)) return false
  const ownerDocument = video.ownerDocument || document
  const active = fullscreenElement(ownerDocument)
  if (!active || !(active === video || (active.contains && active.contains(video)))) return false
  const exit = ownerDocument.exitFullscreen || ownerDocument.webkitExitFullscreen
  if (!exit) return false
  try {
    const result = exit.call(ownerDocument)
    if (result && typeof result.catch === 'function') result.catch(function () {})
    return true
  } catch (_) {
    return false
  }
}

export function toggleFeedVideoFullscreen(video) {
  if (!(video instanceof HTMLVideoElement)) return false
  const ownerDocument = video.ownerDocument || document
  if (fullscreenElement(ownerDocument)) return exitFeedVideoFullscreen(video)
  const request = video.requestFullscreen || video.webkitRequestFullscreen
  if (!request) return false
  try {
    const result = request.call(video)
    if (result && typeof result.catch === 'function') result.catch(function () {})
    return true
  } catch (_) {
    return false
  }
}

export function toggleFeedVideoMute(video) {
  if (!(video instanceof HTMLVideoElement)) return false
  video.muted = !video.muted
  return true
}

export function handleFeedVideoShortcut(event, video, options) {
  if (!event || !(video instanceof HTMLVideoElement)) return false
  const key = String(event.key || '')
  const seekEnabled = !options || options.seek !== false
  if (seekEnabled && (key === 'ArrowLeft' || key === 'ArrowRight')) {
    const delta = key === 'ArrowRight' ? 5 : -5
    const duration = Number(video.duration)
    const upper = Number.isFinite(duration) && duration > 0 ? duration : Infinity
    video.currentTime = Math.max(0, Math.min(upper, Number(video.currentTime || 0) + delta))
    return true
  }
  if (key === 'ArrowUp' || key === 'ArrowDown') {
    const delta = key === 'ArrowUp' ? 0.05 : -0.05
    video.volume = Math.max(0, Math.min(1, Number(video.volume || 0) + delta))
    if (key === 'ArrowUp') video.muted = false
    writeStoredVolume(window.localStorage, FEED_VOLUME_KEY, video.volume)
    return true
  }
  if (key === ' ') {
    if (video.paused) video.play().catch(function () {})
    else video.pause()
    return true
  }
  const shortcuts = window.cfShortcuts
  if (shortcuts && shortcuts.match('feed.mute', key)) return toggleFeedVideoMute(video)
  return false
}

export function bindFeedVideoControls(wrap, video, options) {
  const opts = options || {}
  if (!wrap || !(video instanceof HTMLVideoElement)) return
  const controls = wrap.querySelector('[data-feed-video-controls]')
  if (!controls || controls.dataset.feedVideoControlsBound === '1') return
  controls.dataset.feedVideoControlsBound = '1'
  const volumeKey = opts.volumeKey || FEED_VOLUME_KEY
  video.volume = readStoredVolume(window.localStorage, volumeKey, video.volume)

  const play = controls.querySelector('[data-feed-video-play]')
  const mute = controls.querySelector('[data-feed-video-mute]')
  const volume = controls.querySelector('[data-feed-video-volume]')
  const volumeControl = controls.querySelector('[data-feed-video-volume-control]')
  const volumePopover = controls.querySelector('.feed-video-volume-popover')
  const autoplay = controls.querySelector('[data-feed-video-autoplay]')
  const mini = controls.querySelector('[data-feed-video-mini]')
  const cinema = controls.querySelector('[data-feed-video-cinema]')
  const fullscreen = controls.querySelector('[data-feed-video-fullscreen]')
  const speed = controls.querySelector('[data-feed-video-speed]')
  const speedButton = controls.querySelector('[data-feed-video-speed-button]')
  const speedMenu = controls.querySelector('[data-feed-video-speed-menu]')
  const speedOptions = speedMenu ? Array.from(speedMenu.querySelectorAll('[data-rate]')) : []
  const progress = controls.querySelector('[data-feed-progress]')
  const fill = controls.querySelector('[data-feed-progress-fill]')

  function setVolumeOpen(open) {
    if (!volumeControl) return
    if (open) volumeControl.setAttribute('data-feed-video-volume-open', '')
    else volumeControl.removeAttribute('data-feed-video-volume-open')
  }

  bindVideoControlsVisibility({
    stateElement: controls,
    surface: wrap,
    popupElements: [speedMenu],
    readyAttribute: 'data-feed-video-controls-ready',
    visibleAttribute: 'data-feed-video-controls-visible',
    onVisibilityChange: function (visible) {
      if (!visible) setVolumeOpen(false)
    },
  })

  function syncPlay() {
    if (!play) return
    const paused = video.paused
    play.setAttribute('aria-label', paused ? t('action_play', 'Play') : t('action_pause', 'Pause'))
    setSvgContent(play, videoControlIcons[paused ? 'play' : 'pause'])
  }

  function syncMute() {
    if (mute) {
      mute.setAttribute('aria-label', video.muted ? t('action_unmute', 'Unmute') : t('action_mute', 'Mute'))
      setSvgContent(mute, videoControlIcons[volumeIconLevel(video.muted, video.volume)])
    }
    if (volume) {
      const effectiveVolume = video.muted ? 0 : video.volume
      volume.value = String(effectiveVolume)
      if (volumePopover) volumePopover.style.setProperty('--feed-volume-height', Math.round(effectiveVolume * 62) + 'px')
    }
  }

  function syncSpeed() {
    const current = Number(video.playbackRate || 1)
    if (speedButton) speedButton.textContent = formatPlaybackRate(current)
    speedOptions.forEach(function (option) {
      const active = Math.abs(Number(option.getAttribute('data-rate')) - current) < 0.001
      option.classList.toggle('is-active', active)
      option.setAttribute('aria-checked', active ? 'true' : 'false')
    })
  }

  function syncAutoplay() {
    if (!autoplay) return
    const active = typeof opts.getAutoplay === 'function' ? !!opts.getAutoplay() : false
    autoplay.classList.toggle('active', active)
    autoplay.setAttribute('aria-pressed', active ? 'true' : 'false')
    const label = tf('shorts_autoplay_next_state', 'Auto-play next short: %1$s', active ? t('state_on', 'ON') : t('state_off', 'OFF'))
    autoplay.setAttribute('title', label)
    autoplay.setAttribute('aria-label', label)
    setSvgContent(autoplay, videoControlIcons[active ? 'autoplayActive' : 'autoplay'])
  }

  function syncProgress() {
    if (!fill) return
    const duration = Number(video.duration || 0)
    const current = Number(video.currentTime || 0)
    const percent = duration > 0 ? Math.max(0, Math.min(100, (current / duration) * 100)) : 0
    fill.style.width = percent + '%'
  }

  function closeSpeedMenu() {
    if (!speedMenu || !speedButton) return
    speedMenu.classList.add('hidden')
    speedButton.setAttribute('aria-expanded', 'false')
  }

  if (play) {
    play.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      if (typeof opts.onPlayToggle === 'function') {
        opts.onPlayToggle()
        return
      }
      if (video.paused) video.play().catch(function () {})
      else video.pause()
    })
  }
  if (mute) {
    mute.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      video.muted = !video.muted
      if (typeof opts.onVolumeChange === 'function') {
        opts.onVolumeChange(video.volume, video.muted)
      }
      syncMute()
    })
  }
  if (volume) {
    volume.addEventListener('input', function (event) {
      event.stopPropagation()
      const nextVolume = Math.max(0, Math.min(1, Number(volume.value || 0)))
      video.volume = nextVolume
      video.muted = nextVolume === 0
      writeStoredVolume(window.localStorage, volumeKey, nextVolume)
      if (typeof opts.onVolumeChange === 'function') {
        opts.onVolumeChange(nextVolume, video.muted)
      }
      syncMute()
    })
    volume.addEventListener('click', function (event) { event.stopPropagation() })
    volume.addEventListener('mousedown', function (event) { event.stopPropagation() })
    volume.addEventListener('touchstart', function (event) { event.stopPropagation() })
    volume.addEventListener('pointerup', function (event) {
      event.stopPropagation()
      setVolumeOpen(false)
      volume.blur()
    })
    volume.addEventListener('pointercancel', function () {
      setVolumeOpen(false)
      volume.blur()
    })
  }
  if (volumeControl) {
    volumeControl.addEventListener('pointerenter', function () { setVolumeOpen(true) })
    volumeControl.addEventListener('pointerleave', function () { setVolumeOpen(false) })
    volumeControl.addEventListener('focusin', function () { setVolumeOpen(true) })
    volumeControl.addEventListener('focusout', function (event) {
      if (!volumeControl.contains(event.relatedTarget)) setVolumeOpen(false)
    })
    volumeControl.addEventListener('click', function (event) { event.stopPropagation() })
    volumeControl.addEventListener('mousedown', function (event) { event.stopPropagation() })
    volumeControl.addEventListener('touchstart', function (event) { event.stopPropagation() })
  }
  if (autoplay) {
    autoplay.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      if (typeof opts.onAutoplayToggle === 'function') {
        opts.onAutoplayToggle()
      }
      syncAutoplay()
    })
  }
  if (mini) {
    mini.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      let manager = null
      try { manager = window.top && window.top.IglooMiniPlayer } catch (_) {}
      if (!manager || typeof manager.toggleSurface !== 'function') return
      manager.toggleSurface({
        element: wrap,
        video: video,
        button: mini,
        kind: 'feed',
      })
    })
  }
  if (cinema) {
    cinema.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      if (options && typeof options.onCinema === 'function') {
        options.onCinema()
        return
      }
      const overlay = wrap.closest && wrap.closest('.feed-media-overlay')
      const manager = window.FeedMediaOverlay
      if (!manager) return
      if (overlay && typeof manager.close === 'function') manager.close()
      else if (typeof manager.open === 'function') manager.open(wrap, wrap)
    })
  }
  if (speed && speedButton && speedMenu) {
    speedButton.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      const opening = speedMenu.classList.contains('hidden')
      speedMenu.classList.toggle('hidden', !opening)
      speedButton.setAttribute('aria-expanded', opening ? 'true' : 'false')
    })
    speedOptions.forEach(function (option) {
      option.addEventListener('click', function (event) {
        event.preventDefault()
        event.stopPropagation()
        const rate = Number(option.getAttribute('data-rate') || 1)
        if (!playbackRates.includes(rate)) return
        video.playbackRate = rate
        video.defaultPlaybackRate = rate
        if (typeof opts.onRateChange === 'function') {
          opts.onRateChange(rate)
        }
        syncSpeed()
        closeSpeedMenu()
      })
    })
    speed.addEventListener('focusout', function (event) {
      if (!speed.contains(event.relatedTarget)) closeSpeedMenu()
    })
    speed.addEventListener('click', function (event) { event.stopPropagation() })
    speed.addEventListener('mousedown', function (event) { event.stopPropagation() })
    speed.addEventListener('touchstart', function (event) { event.stopPropagation() })
  }
  function syncFullscreen() {
    if (!fullscreen) return
    const ownerDocument = wrap.ownerDocument || document
    const active = fullscreenElement(ownerDocument) === video
    fullscreen.setAttribute('aria-label', active ? t('action_exit_fullscreen', 'Exit fullscreen') : t('action_enter_fullscreen', 'Enter fullscreen'))
  }
  if (fullscreen) {
    fullscreen.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      if (typeof opts.onFullscreen === 'function') {
        opts.onFullscreen()
        return
      }
      toggleFeedVideoFullscreen(video)
    })
    const ownerDocument = wrap.ownerDocument || document
    ownerDocument.addEventListener('fullscreenchange', syncFullscreen)
    ownerDocument.addEventListener('webkitfullscreenchange', syncFullscreen)
  }

  function exitFullscreenOnDoubleClick(event) {
    if (!exitFeedVideoFullscreen(video)) return
    event.preventDefault()
    event.stopImmediatePropagation()
  }
  video.addEventListener('dblclick', exitFullscreenOnDoubleClick)

  video.addEventListener('play', syncPlay)
  video.addEventListener('pause', syncPlay)
  video.addEventListener('volumechange', syncMute)
  video.addEventListener('ratechange', syncSpeed)
  video.addEventListener('timeupdate', syncProgress)

  makeDraggableSeekbar(progress, fill, video)
  attachSeekTooltip(progress, video)
  syncPlay()
  syncMute()
  syncSpeed()
  syncFullscreen()
  syncAutoplay()
  return function () {
    video.removeEventListener('play', syncPlay)
    video.removeEventListener('pause', syncPlay)
    video.removeEventListener('volumechange', syncMute)
    video.removeEventListener('ratechange', syncSpeed)
    video.removeEventListener('timeupdate', syncProgress)
    video.removeEventListener('dblclick', exitFullscreenOnDoubleClick)
    const ownerDocument = wrap.ownerDocument || document
    ownerDocument.removeEventListener('fullscreenchange', syncFullscreen)
    ownerDocument.removeEventListener('webkitfullscreenchange', syncFullscreen)
  }
}
