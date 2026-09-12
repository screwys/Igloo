import { materialIconMarkup, setSvgContent } from './utils.js'
import { volumeIconLevel } from './volume.js'

function getFeedbackIcon(kind) {
  switch (kind) {
    case 'play':
      return materialIconMarkup('PlayArrow')
    case 'pause':
      return materialIconMarkup('Pause')
    case 'muted':
      return materialIconMarkup('VolumeOff')
    case 'low':
      return materialIconMarkup('VolumeDown')
    case 'high':
      return materialIconMarkup('VolumeUp')
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

export function bindVideoFeedback(surface, video, options) {
  if (!surface || !video) return null

  let lastUserActionTime = 0

  function markUserAction() {
    lastUserActionTime = Date.now()
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
    if (Date.now() - lastUserActionTime < 1000) {
      const level = volumeIconLevel(video.muted, video.volume)
      showVideoFeedback(surface, level)
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

  const feedback = {
    showFeedback: function (kind) {
      showVideoFeedback(surface, kind)
    },
    markUserAction: markUserAction,
    destroy: function () {
      if (video._videoFeedback === feedback) {
        video._videoFeedback = null
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
