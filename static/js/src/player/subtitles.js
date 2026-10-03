import { apiFetch, escapeHtml, showToast, t } from '../utils.js'

export function initSubtitles(video, root) {
  const doc = document
  const videoId = root.dataset.videoId
  const button = doc.getElementById('player-cc-btn')
  const menu = doc.getElementById('player-captions-menu')
  const wrap = doc.getElementById('player-captions-menu-wrap')
  const playerWrapper = root.querySelector('.player-wrapper')
  const controller = root.querySelector('media-controller')
  if (!button || !menu || !wrap || !playerWrapper) return
  const preferenceKey = root.dataset.channelId ? 'igloo.player.subtitles.channel.v1:' + root.dataset.channelId : ''
  let preference = { enabled: false }
  try {
    const saved = preferenceKey ? JSON.parse(localStorage.getItem(preferenceKey) || 'null') : null
    if (saved && typeof saved.enabled === 'boolean') preference = saved
  } catch (_) {}
  let tracks = []
  let selected = null
  let selectStreamTrack = null
  let catalogueLoaded = !!root.dataset.streamManifest
  let loadingCatalogue = false
  let subtitleOverlay = null
  let subtitleCues = []
  function savePreference() {
    if (!preferenceKey) return
    try { localStorage.setItem(preferenceKey, JSON.stringify(preference)) } catch (_) {}
  }
  function preferredTrack() {
    const language = preference.language || doc.documentElement.lang || 'en'
    return tracks.find(function (track) { return track.language === language && track.automatic === preference.automatic }) ||
      tracks.find(function (track) { return track.language === language }) || tracks[0] || null
  }
  function readSubtitleOffsetPx(name, fallback) {
    if (!playerWrapper) return fallback
    var raw = window.getComputedStyle(playerWrapper).getPropertyValue(name)
    var value = parseFloat(raw)
    return Number.isFinite(value) ? value : fallback
  }
  function readSubtitleOffset(name, fallback) {
    if (!playerWrapper) return fallback
    var raw = window.getComputedStyle(playerWrapper).getPropertyValue(name).trim()
    var value = parseFloat(raw)
    if (!Number.isFinite(value)) return fallback
    if (raw.endsWith('%')) {
      var rect = playerWrapper.getBoundingClientRect()
      return rect && rect.height > 0 ? rect.height * value / 100 : fallback
    }
    return value
  }
  function controlsSubtitleOffsetPx(isFs) {
    var fallback = readSubtitleOffset(isFs ? '--player-subtitles-offset-fullscreen-controls' : '--player-subtitles-offset-controls', isFs ? 104 : 72)
    if (!controller || !playerWrapper) return fallback
    var bar = controller.querySelector('media-control-bar.dashboard-media-control-bar, media-control-bar, .dashboard-media-control-bar')
    if (!bar || typeof bar.getBoundingClientRect !== 'function') return fallback
    var wrapperRect = playerWrapper.getBoundingClientRect()
    var barRect = bar.getBoundingClientRect()
    if (!(wrapperRect && wrapperRect.height > 0 && barRect && barRect.height > 0)) return fallback
    var gap = readSubtitleOffsetPx(isFs ? '--player-subtitles-controls-gap-fullscreen' : '--player-subtitles-controls-gap', isFs ? 12 : 6)
    var measured = wrapperRect.bottom - barRect.top + gap
    if (!Number.isFinite(measured) || measured <= 0) return fallback
    return Math.max(0, Math.min(wrapperRect.height, measured))
  }
  function subtitleOffsetPx() {
    var owner = video.ownerDocument
    var isFs = !!(owner.fullscreenElement || owner.webkitFullscreenElement)
    var controlsVisible = controller && controller.getAttribute('data-player-controls-visible') === '1'
    if (controlsVisible) return controlsSubtitleOffsetPx(isFs)
    if (isFs) return readSubtitleOffset('--player-subtitles-offset-fullscreen-idle', 52)
    return readSubtitleOffset('--player-subtitles-offset-idle', 36)
  }
  function ensureSubtitleOverlay() {
    if (subtitleOverlay) return subtitleOverlay
    subtitleOverlay = doc.createElement('div')
    subtitleOverlay.className = 'player-subtitle-overlay hidden'
    subtitleOverlay.setAttribute('aria-hidden', 'true')
    playerWrapper.appendChild(subtitleOverlay)
    return subtitleOverlay
  }
  function parseVttTimestamp(raw) {
    var value = String(raw || '').trim().split(/\s+/)[0]
    if (!value) return Number.NaN
    var parts = value.split(':')
    if (parts.length < 2 || parts.length > 3) return Number.NaN
    var secondsPart = parts.pop().replace(',', '.')
    var minutesPart = parts.pop()
    var hoursPart = parts.length ? parts.pop() : '0'
    var hours = Number(hoursPart)
    var minutes = Number(minutesPart)
    var seconds = Number(secondsPart)
    if (!Number.isFinite(hours) || !Number.isFinite(minutes) || !Number.isFinite(seconds)) return Number.NaN
    return hours * 3600 + minutes * 60 + seconds
  }
  function parseVtt(text) {
    var lines = String(text || '').replace(/\r/g, '').split('\n')
    var cues = []
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim()
      if (!line) continue
      if (line === 'WEBVTT' || line.indexOf('Kind:') === 0 || line.indexOf('Language:') === 0) continue
      if (line.indexOf('-->') < 0) continue
      var parts = line.split('-->')
      var start = parseVttTimestamp(parts[0])
      var end = parseVttTimestamp(parts[1])
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue
      var cueLines = []
      for (i = i + 1; i < lines.length; i++) {
        var cueLine = lines[i]
        if (!cueLine.trim()) break
        cueLines.push(cueLine)
      }
      if (cueLines.length) cues.push({ start: start, end: end, text: cueLines.join('\n') })
    }
    return cues
  }
  function subtitleTextHtml(text) {
    return escapeHtml(sanitizeVttCueText(text).replace(/\s*\r?\n\s*/g, ' '))
  }
  function sanitizeVttCueText(text) {
    return decodeVttEntities(
      String(text || '')
        .replace(/<(?:\d{1,2}:)?\d{2}:\d{2}[.,]\d{3}>/g, '')
        .replace(/<\/?(?:c(?:\.[^>\s]+)*|v(?:\s+[^>]*)?|lang(?:\s+[^>]*)?|b|i|u|ruby|rt)>/g, '')
        .replace(/<[^>]+>/g, '')
    ).trim()
  }
  function decodeVttEntities(text) {
    return String(text || '')
      .replace(/&nbsp;|&#160;|&#x0*a0;/gi, ' ')
      .replace(/&amp;/gi, '&')
      .replace(/&lt;/gi, '<')
      .replace(/&gt;/gi, '>')
      .replace(/&quot;/gi, '"')
      .replace(/&apos;|&#39;/gi, "'")
  }
  function updateSubtitleOverlayPosition() {
    var overlay = ensureSubtitleOverlay()
    overlay.style.bottom = subtitleOffsetPx() + 'px'
  }
  function activeSubtitleCues() {
    var time = Number(video.currentTime || 0)
    if (!Number.isFinite(time)) return []
    return subtitleCues.filter(function (cue) {
      return time >= cue.start && time < cue.end
    })
  }
  function renderSubtitleOverlay() {
    var overlay = ensureSubtitleOverlay()
    updateSubtitleOverlayPosition()
    var activeCues = selected ? activeSubtitleCues() : []
    if (!activeCues.length) {
      overlay.classList.add('hidden')
      overlay.replaceChildren()
      return
    }
    var html = []
    for (var i = 0; i < activeCues.length; i++) {
      var cue = activeCues[i]
      html.push('<div class="player-subtitle-cue">' + subtitleTextHtml(cue && cue.text) + '</div>')
    }
    overlay.innerHTML = html.join('')
    overlay.classList.remove('hidden')
  }
  if (controller) {
    new MutationObserver(function () {
      renderSubtitleOverlay()
    }).observe(controller, { attributes: true, attributeFilter: ['userinactive', 'data-player-controls-visible'] })
    controller.addEventListener('playercontrolsvisibilitychange', function () {
      requestAnimationFrame(renderSubtitleOverlay)
    })
  }
  video.addEventListener('timeupdate', renderSubtitleOverlay, { passive: true })
  video.addEventListener('seeked', renderSubtitleOverlay)
  video.addEventListener('play', renderSubtitleOverlay)
  video.addEventListener('pause', renderSubtitleOverlay)
  video.addEventListener('loadedmetadata', renderSubtitleOverlay)
  window.addEventListener('resize', function () { renderSubtitleOverlay() }, { passive: true })
  doc.addEventListener('fullscreenchange', function () { requestAnimationFrame(renderSubtitleOverlay) })
  doc.addEventListener('webkitfullscreenchange', function () { requestAnimationFrame(renderSubtitleOverlay) })
  root.addEventListener('scroll', function () { requestAnimationFrame(renderSubtitleOverlay) }, { passive: true })


  function renderMenu() {
    const focused = menu.contains(doc.activeElement) ? doc.activeElement.dataset.caption : null
    menu.replaceChildren()
    const options = [{ id: 'off', label: t('option_off', 'Off') }].concat(tracks)
    options.forEach(function (track) {
      const option = doc.createElement('button')
      option.type = 'button'
      option.className = 'mc-speed-option'
      option.dataset.caption = track.id
      option.setAttribute('role', 'menuitemradio')
      option.setAttribute('aria-checked', String(track.id === (selected ? selected.id : 'off')))
      option.textContent = track.label
      menu.appendChild(option)
      if (focused === track.id) option.focus()
    })
    button.classList.toggle('active', !!selected)
    button.title = t('player_subtitles', 'Subtitles') + ' (' + (selected ? selected.label : t('option_off', 'Off')) + ')'
    button.setAttribute('aria-label', button.title)
    wrap.classList.toggle('hidden', tracks.length === 0 && (catalogueLoaded || root.dataset.channelPlatform !== 'youtube'))
  }

  async function selectTrack(track) {
    selected = track
    subtitleCues = []
    renderMenu()
    renderSubtitleOverlay()
    try {
      if (selectStreamTrack) {
        await selectStreamTrack(track)
        return
      }
      if (!track) return
      const response = await fetch(track.url, { credentials: 'same-origin' })
      if (!response.ok) throw new Error('caption download failed')
      const text = await response.text()
      if (selected !== track) return
      subtitleCues = parseVtt(text)
      renderSubtitleOverlay()
    } catch (_) {
      if (selected === track) showToast(t('stream_subtitles_failed', 'Subtitles could not load.'))
    }
  }

  function sourceTracks(list) {
    return list.map(function (track) {
      const label = track.label || track.language
      return { id: track.url, url: track.url, language: track.language, automatic: !!track.automatic,
        label: track.automatic && !label.endsWith(' (auto)') ? label + ' (auto)' : label }
    })
  }

  function updateTracks(incoming) {
    tracks = incoming
    if (selectStreamTrack || !root.dataset.streamManifest) {
      const next = preference.enabled ? preferredTrack() : null
      if ((next && (!selected || next.url !== selected.url)) || (!next && selected)) selectTrack(next)
    }
    renderMenu()
  }

  menu.addEventListener('click', function (event) {
    const option = event.target.closest('[data-caption]')
    if (!option) return
    event.preventDefault()
    menu.classList.add('hidden')
    button.setAttribute('aria-expanded', 'false')
    button.focus()
    const track = tracks.find(function (track) { return track.id === option.dataset.caption }) || null
    preference.enabled = !!track
    if (track) {
      preference.language = track.language
      preference.automatic = track.automatic
    }
    savePreference()
    selectTrack(track)
  })
  root.addEventListener('captiontrackschanged', function (event) {
    catalogueLoaded = !event.detail.refresh
    selectStreamTrack = event.detail.selectTrack || null
    selected = null
    updateTracks(sourceTracks(event.detail.tracks))
  })
  async function loadCatalogue() {
    if (catalogueLoaded || loadingCatalogue || root.dataset.channelPlatform !== 'youtube') return
    loadingCatalogue = true
    button.setAttribute('aria-busy', 'true')
    try {
      const response = await apiFetch('/api/youtube/' + encodeURIComponent(videoId) + '/captions', { method: 'POST', body: '{}' })
      const incoming = sourceTracks(response.text_tracks || [])
      const saved = tracks
      updateTracks(saved.concat(incoming.filter(function (track) {
        return !saved.some(function (local) { return local.language === track.language && local.automatic === track.automatic })
      })))
      catalogueLoaded = true
      renderMenu()
    } catch (_) {
      showToast(t('stream_subtitles_failed', 'Subtitles could not load.'))
    } finally {
      loadingCatalogue = false
      button.removeAttribute('aria-busy')
    }
  }
  button.addEventListener('click', loadCatalogue)

  if (root.dataset.streamManifest) {
    updateTracks(sourceTracks(JSON.parse(root.dataset.streamTextTracks || '[]')))
  } else {
    renderMenu()
    apiFetch('/api/videos/' + encodeURIComponent(videoId) + '/subtitles').then(function (response) {
      const saved = (response.tracks || []).map(function (track) {
        const label = track.label || track.srclang
        return { id: track.track_id, language: track.srclang, automatic: !!track.is_auto,
          url: '/api/media/subtitle/' + encodeURIComponent(videoId) + '?track=' + encodeURIComponent(track.track_id),
          label: track.is_auto && !label.endsWith(' (auto)') ? label + ' (auto)' : label }
      })
      updateTracks(saved.concat(tracks.filter(function (track) {
        return !saved.some(function (local) { return local.language === track.language && local.automatic === track.automatic })
      })))
      if (preference.enabled && (!tracks.length || preference.language && !tracks.some(function (track) {
        return track.language === preference.language && track.automatic === preference.automatic
      }))) loadCatalogue()
    }).catch(function () {})
  }
  async function toggleSubtitles() {
    preference.enabled = !preference.enabled
    savePreference()
    if (!preference.enabled) { selectTrack(null); return }
    if (!tracks.length) await loadCatalogue()
    if (preference.enabled && (selectStreamTrack || !root.dataset.streamManifest)) selectTrack(preferredTrack())
  }
  video.addEventListener('togglesubtitles', toggleSubtitles)
}
