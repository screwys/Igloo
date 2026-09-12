import { materialIconMarkup, setSvgContent } from './utils.js'

function getFeedbackIcon(kind) {
  switch (kind) {
    case 'play':
      return materialIconMarkup('PlayArrow')
    case 'pause':
      return materialIconMarkup('Pause')
    default:
      return typeof kind === 'string' && kind.startsWith('<') ? kind : materialIconMarkup(kind)
  }
}

export function ensureFeedbackBezel(surface) {
  if (!surface) return null
  let bezel = null
  if (typeof surface.querySelector === 'function') {
    bezel = surface.querySelector('[data-video-feedback-bezel]')
  }
  if (!bezel) {
    const doc = surface.ownerDocument || document
    bezel = doc.createElement('div')
    bezel.className = 'video-feedback-bezel'
    bezel.setAttribute('data-video-feedback-bezel', '')
    bezel.setAttribute('aria-hidden', 'true')
    surface.appendChild(bezel)
  }
  return bezel
}

export function ensureVolumeBezel(surface) {
  if (!surface) return null
  let bezel = null
  if (typeof surface.querySelector === 'function') {
    bezel = surface.querySelector('[data-video-volume-bezel]')
  }
  if (!bezel) {
    const doc = surface.ownerDocument || document
    bezel = doc.createElement('div')
    bezel.className = 'video-volume-bezel'
    bezel.setAttribute('data-video-volume-bezel', '')
    bezel.setAttribute('aria-hidden', 'true')
    surface.appendChild(bezel)
  }
  return bezel
}

export function showVideoFeedback(surface, iconKind) {
  if (!surface) return null
  const bezel = ensureFeedbackBezel(surface)
  if (!bezel) return null
  const svg = getFeedbackIcon(iconKind)
  if (svg) setSvgContent(bezel, svg)
  if (bezel.classList && typeof bezel.classList.remove === 'function' && typeof bezel.classList.add === 'function') {
    bezel.classList.remove('is-animating')
    void bezel.offsetWidth
    bezel.classList.add('is-animating')
  }
  if (typeof bezel.addEventListener === 'function') {
    const onEnd = function () {
      if (bezel.classList && typeof bezel.classList.remove === 'function') {
        bezel.classList.remove('is-animating')
      }
      if (typeof bezel.removeEventListener === 'function') {
        bezel.removeEventListener('animationend', onEnd)
      }
    }
    bezel.addEventListener('animationend', onEnd)
  }
  return bezel
}

export function showVideoVolumeFeedback(surface, percent) {
  if (!surface) return null
  const bezel = ensureVolumeBezel(surface)
  if (!bezel) return null

  bezel.textContent = String(percent)
  if (bezel.classList && typeof bezel.classList.add === 'function') {
    bezel.classList.add('is-visible')
  }

  if (bezel._hideTimeout) {
    clearTimeout(bezel._hideTimeout)
  }
  bezel._hideTimeout = setTimeout(function () {
    if (bezel.classList && typeof bezel.classList.remove === 'function') {
      bezel.classList.remove('is-visible')
    }
    bezel._hideTimeout = null
  }, 800)

  return bezel
}

export function bindVideoFeedback(surface, video, options) {
  if (!surface || !video) return null

  let lastUserActionTime = 0
  let lastVolumeActionTime = 0

  function markUserAction(kind) {
    lastUserActionTime = Date.now()
    if (kind === 'volume') lastVolumeActionTime = lastUserActionTime
  }

  function onPointerDown() {
    lastUserActionTime = Date.now()
  }

  function onClick() {
    lastUserActionTime = Date.now()
  }

  function onPlay() {
    if (Date.now() - lastUserActionTime < 1000) {
      showVideoFeedback(surface, 'play')
    }
  }

  function onPause() {
    if (video.ended) return
    if (Date.now() - lastUserActionTime < 1000) {
      showVideoFeedback(surface, 'pause')
    }
  }

  function onVolumeChange() {
    const actionTime = options && options.explicitVolumeActions ? lastVolumeActionTime : lastUserActionTime
    if (Date.now() - actionTime < 1000) {
      const isMuted = Boolean(video.muted)
      const vol = isMuted ? 0 : Math.round((Number(video.volume) || 0) * 100)
      showVideoVolumeFeedback(surface, `${vol}%`)
    }
  }

  if (typeof surface.addEventListener === 'function') {
    surface.addEventListener('pointerdown', onPointerDown, { capture: true, passive: true })
    surface.addEventListener('click', onClick, { capture: true, passive: true })
  }
  if (typeof video.addEventListener === 'function') {
    video.addEventListener('pointerdown', onPointerDown, { capture: true, passive: true })
    video.addEventListener('click', onClick, { capture: true, passive: true })
    video.addEventListener('play', onPlay)
    video.addEventListener('pause', onPause)
    video.addEventListener('volumechange', onVolumeChange)
  }

  const extraElements = (options && options.interactiveElements) || []
  extraElements.forEach(function (el) {
    if (el && typeof el.addEventListener === 'function') {
      el.addEventListener('pointerdown', onPointerDown, { capture: true, passive: true })
      el.addEventListener('click', onClick, { capture: true, passive: true })
    }
  })

  ensureFeedbackBezel(surface)
  ensureVolumeBezel(surface)

  const feedback = {
    showFeedback: function (kind) {
      showVideoFeedback(surface, kind)
    },
    showVolumeFeedback: function (percent) {
      showVideoVolumeFeedback(surface, percent)
    },
    markUserAction: markUserAction,
    destroy: function () {
      if (video._videoFeedback === feedback) {
        video._videoFeedback = null
      }
      const volBezel = surface.querySelector ? surface.querySelector('[data-video-volume-bezel]') : null
      if (volBezel && volBezel._hideTimeout) {
        clearTimeout(volBezel._hideTimeout)
        volBezel._hideTimeout = null
      }
      if (typeof surface.removeEventListener === 'function') {
        surface.removeEventListener('pointerdown', onPointerDown, { capture: true })
        surface.removeEventListener('click', onClick, { capture: true })
      }
      if (typeof video.removeEventListener === 'function') {
        video.removeEventListener('pointerdown', onPointerDown, { capture: true })
        video.removeEventListener('click', onClick, { capture: true })
        video.removeEventListener('play', onPlay)
        video.removeEventListener('pause', onPause)
        video.removeEventListener('volumechange', onVolumeChange)
      }
      extraElements.forEach(function (el) {
        if (el && typeof el.removeEventListener === 'function') {
          el.removeEventListener('pointerdown', onPointerDown, { capture: true })
          el.removeEventListener('click', onClick, { capture: true })
        }
      })
    },
  }

  video._videoFeedback = feedback
  return feedback
}
