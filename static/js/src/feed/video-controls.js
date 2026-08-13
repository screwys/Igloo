import { attachSeekTooltip, makeDraggableSeekbar, setSvgContent, t } from '../utils.js'
import { bindVideoControlsVisibility } from '../video-controls-visibility.js'

const videoControlIcons = {
  play: '<svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5v14l11-7z"/></svg>',
  pause: '<svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M6 5h4v14H6zM14 5h4v14h-4z"/></svg>',
  muted: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M11 5 6 9H2v6h4l5 4V5z"/><path d="m22 9-6 6M16 9l6 6"/></svg>',
  unmuted: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M11 5 6 9H2v6h4l5 4V5z"/><path d="M15.5 8.5a5 5 0 0 1 0 7M19 5a10 10 0 0 1 0 14"/></svg>',
  expand: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M8 3H3v5M16 3h5v5M21 16v5h-5M3 16v5h5"/></svg>'
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
  control.appendChild(popover)
  control.appendChild(makeControlButton('data-feed-video-mute', t('action_mute', 'Mute'), 'unmuted'))
  return control
}

function makeSpeedControl() {
  const control = document.createElement('div')
  control.className = 'feed-video-speed-menu-wrap'
  control.setAttribute('data-feed-video-speed', '')

  const button = document.createElement('button')
  button.className = 'feed-video-control-btn feed-video-speed-btn'
  button.type = 'button'
  button.textContent = '1x'
  button.setAttribute('aria-label', t('player_playback_speed', 'Playback speed'))
  button.setAttribute('aria-haspopup', 'menu')
  button.setAttribute('aria-expanded', 'false')
  button.setAttribute('data-feed-video-control', '')
  button.setAttribute('data-feed-video-speed-button', '')
  control.appendChild(button)

  const menu = document.createElement('div')
  menu.className = 'feed-video-speed-menu hidden'
  menu.setAttribute('role', 'menu')
  menu.setAttribute('aria-label', t('player_playback_speed_menu', 'Playback speed menu'))
  menu.setAttribute('data-feed-video-speed-menu', '')
  playbackRates.forEach(function (rate) {
    const option = document.createElement('button')
    option.className = 'feed-video-speed-option' + (rate === 1 ? ' is-active' : '')
    option.type = 'button'
    option.textContent = formatPlaybackRate(rate)
    option.setAttribute('role', 'menuitemradio')
    option.setAttribute('aria-checked', rate === 1 ? 'true' : 'false')
    option.setAttribute('data-rate', String(rate))
    menu.appendChild(option)
  })
  control.appendChild(menu)
  return control
}

export function createFeedVideoControls(options) {
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

  controls.appendChild(makeSpeedControl())
  controls.appendChild(makeVolumeControl())
  if (options && typeof options.onExpand === 'function') {
    controls.appendChild(makeControlButton('data-feed-video-expand', t('action_enter_fullscreen', 'Enter fullscreen'), 'expand'))
  }
  return controls
}

export function bindFeedVideoControls(wrap, video, options) {
  if (!wrap || !(video instanceof HTMLVideoElement)) return
  const controls = wrap.querySelector('[data-feed-video-controls]')
  if (!controls || controls.dataset.feedVideoControlsBound === '1') return
  controls.dataset.feedVideoControlsBound = '1'

  const play = controls.querySelector('[data-feed-video-play]')
  const mute = controls.querySelector('[data-feed-video-mute]')
  const volume = controls.querySelector('[data-feed-video-volume]')
  const volumePopover = controls.querySelector('.feed-video-volume-popover')
  const expand = controls.querySelector('[data-feed-video-expand]')
  const speed = controls.querySelector('[data-feed-video-speed]')
  const speedButton = controls.querySelector('[data-feed-video-speed-button]')
  const speedMenu = controls.querySelector('[data-feed-video-speed-menu]')
  const speedOptions = speedMenu ? Array.from(speedMenu.querySelectorAll('[data-rate]')) : []
  const progress = controls.querySelector('[data-feed-progress]')
  const fill = controls.querySelector('[data-feed-progress-fill]')

  bindVideoControlsVisibility({
    stateElement: controls,
    surface: wrap,
    popupElements: [speedMenu],
    readyAttribute: 'data-feed-video-controls-ready',
    visibleAttribute: 'data-feed-video-controls-visible',
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
      setSvgContent(mute, videoControlIcons[video.muted ? 'muted' : 'unmuted'])
    }
    if (volume) {
      const effectiveVolume = video.muted ? 0 : video.volume
      volume.value = String(effectiveVolume)
      if (volumePopover) volumePopover.style.setProperty('--feed-volume-height', Math.round(effectiveVolume * 76) + 'px')
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

  function closeSpeedMenu() {
    if (!speedMenu || !speedButton) return
    speedMenu.classList.add('hidden')
    speedButton.setAttribute('aria-expanded', 'false')
  }

  if (play) {
    play.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      if (video.paused) video.play().catch(function () {})
      else video.pause()
    })
  }
  if (mute) {
    mute.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      video.muted = !video.muted
    })
  }
  if (volume) {
    volume.addEventListener('input', function (event) {
      event.stopPropagation()
      const nextVolume = Math.max(0, Math.min(1, Number(volume.value || 0)))
      video.volume = nextVolume
      video.muted = nextVolume === 0
      syncMute()
    })
    volume.addEventListener('click', function (event) { event.stopPropagation() })
    volume.addEventListener('mousedown', function (event) { event.stopPropagation() })
    volume.addEventListener('touchstart', function (event) { event.stopPropagation() })
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
  if (expand && options && typeof options.onExpand === 'function') {
    expand.addEventListener('click', function (event) {
      event.preventDefault()
      event.stopPropagation()
      options.onExpand()
    })
  }

  video.addEventListener('play', syncPlay)
  video.addEventListener('pause', syncPlay)
  video.addEventListener('volumechange', syncMute)
  video.addEventListener('ratechange', syncSpeed)
  video.addEventListener('timeupdate', function () {
    if (!fill) return
    const duration = Number(video.duration || 0)
    const current = Number(video.currentTime || 0)
    const percent = duration > 0 ? Math.max(0, Math.min(100, (current / duration) * 100)) : 0
    fill.style.width = percent + '%'
  })

  makeDraggableSeekbar(progress, fill, video)
  attachSeekTooltip(progress, video)
  syncPlay()
  syncMute()
  syncSpeed()
}
