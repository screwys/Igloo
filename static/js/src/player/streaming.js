import { apiFetch, showToast, t } from '../utils.js'

export async function initStreaming(video, root, autoplay, resumePosition) {
  const quality = document.getElementById('player-quality-select')
  const download = document.getElementById('player-stream-download-btn')
  const controller = root.querySelector('media-controller')
  const captions = document.getElementById('player-cc-btn')
  let loaded = false
  let wantsPlay = autoplay
  let started = false
  let manualTrack = null
  let player
  let refreshed = false
  let refreshing = null
  let nativePlayback = false
  let nativeCaptions
  let selectedTextTrack
  let preferIndexed = root.dataset.streamIndexed === '1'
  let handleFailure = async function (error) { fail(error) }

  function fail(error) {
		showToast(t('stream_playback_failed', 'Streaming failed. You can download this video to watch it in Igloo.'))
    console.debug('[Player] stream failed', error && error.code)
  }
  if (download) download.addEventListener('click', async function () {
    download.disabled = true
    try {
      const result = await apiFetch('/api/quick-download', {
        method: 'POST', body: JSON.stringify({ url: root.dataset.originalUrl }),
      })
      if (!result || !result.success) throw new Error(result && (result.error || result.message) || t('temp_download_status_failed', 'Download failed'))
      showToast(t('stream_download_queued', 'Download queued. Streaming will continue.'))
    } catch (error) {
      showToast(error.message || t('temp_download_status_failed', 'Download failed'))
      download.disabled = false
    }
  })
  if (controller) {
    controller.addEventListener('mediaplayrequest', function (event) {
      wantsPlay = true
      if (!loaded) event.stopImmediatePropagation()
    }, true)
    controller.addEventListener('mediapauserequest', function () { wantsPlay = false }, true)
  }
  video.addEventListener('pause', function () { if (loaded) wantsPlay = false })
  video.addEventListener('play', function () { wantsPlay = true })
  try {
    const shaka = window.shaka
    if (!shaka) throw new Error('stream player unavailable')
    shaka.polyfill.installAll()
    if (!shaka.Player.isBrowserSupported()) throw new Error('streaming unsupported')
    player = new shaka.Player()
    await player.attach(video)

    function configure() {
      if (nativePlayback) return
      const preferences = window.IglooPlayback.read()
      const buffer = preferences.buffers.youtube
      player.configure({
        abr: { enabled: manualTrack === null, restrictions: { maxHeight: preferences.cap || Infinity } },
        streaming: { bufferingGoal: buffer.ahead, rebufferingGoal: started ? buffer.refill : buffer.startup },
      })
    }
    configure()
    video.addEventListener('playing', function () {
      if (!started) { started = true; configure() }
    })
    window.addEventListener('iglooplaybackpreferenceschange', configure)
    function renderQualities() {
      if (!quality) return
      const current = manualTrack === null ? 'auto' : String(manualTrack)
      const tracks = player.getVariantTracks().slice().sort(function (a, b) { return (b.height - a.height) || (b.bandwidth - a.bandwidth) })
      if (quality.options.length === tracks.length + 1 && tracks.every(function (track, index) { return quality.options[index + 1].value === String(track.id) })) {
        quality.value = current
        quality.disabled = !loaded
        return
      }
      quality.replaceChildren()
      quality.add(new Option(t('player_quality_auto', 'Auto'), 'auto'))
      tracks.forEach(function (track) {
        const resolution = track.height ? track.height + 'p' : t('player_quality_audio', 'Audio')
        const rate = track.frameRate > 30 ? ' ' + Math.round(track.frameRate) + 'fps' : ''
        const codec = track.videoCodec ? ' ' + track.videoCodec.split('.')[0] : ''
        const language = track.language && track.language !== 'und' ? ' ' + track.language : ''
        quality.add(new Option(resolution + rate + codec + language, String(track.id)))
      })
      quality.value = current
      quality.disabled = !loaded
    }
    if (quality) quality.addEventListener('change', function () {
      manualTrack = quality.value === 'auto' ? null : Number(quality.value)
      configure()
      if (manualTrack !== null) {
        const track = player.getVariantTracks().find(function (candidate) { return candidate.id === manualTrack })
        if (track) player.selectVariantTrack(track, true, window.IglooPlayback.buffer('youtube').refill)
      }
    })
    player.addEventListener('trackschanged', renderQualities)
    function syncCaptions() {
      if (!captions) return
      const on = nativePlayback ? !!nativeCaptions && nativeCaptions.track.mode === 'showing' : player.getTextTracks().some(function (track) { return track.active })
      captions.classList.toggle('active', on)
      captions.title = on ? t('player_subtitles_on', 'Subtitles (On)') : t('player_subtitles', 'Subtitles')
      captions.setAttribute('aria-label', captions.title)
    }
    if (captions) captions.addEventListener('click', function () {
      if (nativePlayback && nativeCaptions) nativeCaptions.track.mode = nativeCaptions.track.mode === 'showing' ? 'disabled' : 'showing'
      else {
        const active = player.getTextTracks().find(function (track) { return track.active })
        if (active) selectedTextTrack = active
        player.selectTextTrack(active ? null : selectedTextTrack)
      }
      syncCaptions()
    })
    player.addEventListener('textchanged', syncCaptions)
    function automaticCaption(track) {
      return String(track.originalTextId || '').endsWith('_auto') || String(track.label || '').endsWith('(auto)')
    }
    function selectCaptions(savedTrack, visible) {
      const textTracks = player.getTextTracks()
      if (!captions || !textTracks.length) return
      const language = (document.documentElement.lang || 'en').split('-')[0]
      const preferred = textTracks.find(function (track) { return track.language.split('-')[0] === language })
      const manual = textTracks.find(function (track) { return !automaticCaption(track) && track.language.split('-')[0] === language }) || textTracks.find(function (track) { return !automaticCaption(track) })
      selectedTextTrack = savedTrack && textTracks.find(function (track) { return track.language === savedTrack.language && automaticCaption(track) === automaticCaption(savedTrack) }) || manual || preferred || textTracks[0]
      player.selectTextTrack((visible == null ? !!manual : visible) ? selectedTextTrack : null)
      captions.classList.remove('hidden')
      syncCaptions()
    }
    async function loadCaptions(tracks, savedTrack, visible) {
      if (tracks.length && !player.getTextTracks().length && !player.isLive() && !player.isInProgress()) {
        await Promise.all(tracks.map(function (track) {
          return player.addTextTrackAsync(track.url, track.language, 'subtitles', 'text/vtt', undefined, track.label)
        }))
      }
      selectCaptions(savedTrack, visible)
    }
    function captionFailure(error) {
      showToast(t('stream_subtitles_failed', 'Subtitles could not load.'))
      console.debug('[Player] subtitles failed', error && error.code)
    }

    async function renewSource() {
      const position = loaded ? video.currentTime : resumePosition || 0
      let rate = video.playbackRate
      const selectedQuality = player.getVariantTracks().find(function (track) { return track.id === manualTrack })
      const selectedCaptions = player.getTextTracks().find(function (track) { return track.active })
      const captionsVisible = loaded ? player.getTextTracks().some(function (track) { return track.active }) : null
      loaded = false
      if (quality) quality.disabled = true
      const fresh = await apiFetch('/api/youtube/' + encodeURIComponent(root.dataset.videoId) + '/stream', { method: 'POST', body: JSON.stringify({ prefer_indexed: preferIndexed }) })
      if (!fresh || !fresh.success) throw new Error('stream renewal failed')
      if (fresh.media_url) {
        nativePlayback = true
        await player.unload()
        root.dataset.streamManifest = ''
        root.dataset.streamSessionId = ''
        if (quality) quality.classList.add('hidden')
        if (download) download.classList.add('hidden')
        await new Promise(function (resolve, reject) {
          function metadata() { video.removeEventListener('error', error); resolve() }
          function error() { video.removeEventListener('loadedmetadata', metadata); reject(new Error('stored video failed')) }
          video.addEventListener('loadedmetadata', metadata, { once: true })
          video.addEventListener('error', error, { once: true })
          video.src = fresh.media_url
          video.load()
        })
        video.currentTime = position
        rate = video.defaultPlaybackRate || rate
        video.defaultPlaybackRate = rate
        video.playbackRate = rate
        loaded = true
        if (wantsPlay) video.play().catch(function () { wantsPlay = false })
        if (captions) {
          const response = await apiFetch('/api/videos/' + encodeURIComponent(root.dataset.videoId) + '/subtitles').catch(function () { return { tracks: [] } })
          const tracks = response.tracks || []
          const auto = selectedCaptions && automaticCaption(selectedCaptions)
          const track = selectedCaptions
            ? tracks.find(function (candidate) { return candidate.srclang === selectedCaptions.language && !!candidate.is_auto === !!auto })
            : tracks.find(function (candidate) { return !candidate.is_auto }) || tracks[0]
          if (track) {
            nativeCaptions = document.createElement('track')
            nativeCaptions.kind = 'subtitles'
            nativeCaptions.label = track.label || track.srclang
            nativeCaptions.srclang = track.srclang
            nativeCaptions.src = '/api/media/subtitle/' + encodeURIComponent(root.dataset.videoId) + '?track=' + encodeURIComponent(track.track_id)
            video.appendChild(nativeCaptions)
            nativeCaptions.track.mode = (captionsVisible == null ? !track.is_auto : captionsVisible) ? 'showing' : 'disabled'
          } else if (captions) captions.classList.add('hidden')
          syncCaptions()
        }
        return
      }
      if (!fresh.manifest_url) throw new Error('stream renewal failed')
      root.dataset.streamManifest = fresh.manifest_url
      root.dataset.streamManifestType = fresh.manifest_type
      root.dataset.streamSessionId = fresh.session_id
      await player.load(fresh.manifest_url, position, fresh.manifest_type === 'hls' ? 'application/x-mpegurl' : 'application/dash+xml')
      if (selectedQuality) {
        const track = player.getVariantTracks().find(function (candidate) {
          const sameVideo = selectedQuality.originalVideoId != null
            ? candidate.originalVideoId === selectedQuality.originalVideoId
            : candidate.height === selectedQuality.height && candidate.width === selectedQuality.width && candidate.videoCodec === selectedQuality.videoCodec && candidate.frameRate === selectedQuality.frameRate
          return sameVideo && candidate.language === selectedQuality.language && candidate.audioCodec === selectedQuality.audioCodec
        })
        manualTrack = track ? track.id : null
        if (track) player.selectVariantTrack(track)
      }
      configure()
      rate = video.defaultPlaybackRate || rate
      video.defaultPlaybackRate = rate
      video.playbackRate = rate
      loaded = true
      renderQualities()
      if (wantsPlay) video.play().catch(function () { wantsPlay = false })
      loadCaptions(fresh.text_tracks || [], selectedCaptions, captionsVisible).catch(captionFailure)
    }
    function sourceExpired(error) {
      const data = error && error.data || []
      const httpStatus = Number(data[1])
      return error && error.code === shaka.util.Error.Code.BAD_HTTP_STATUS && ([403, 410].includes(httpStatus) || httpStatus === 404 && String(data[0]).includes('/api/youtube/streams/'))
    }
    handleFailure = async function (error) {
      if (nativePlayback) return
      if (refreshing) return
      if (error && error.code === shaka.util.Error.Code.CONTENT_UNSUPPORTED_BY_BROWSER && !preferIndexed) preferIndexed = true
      else if (sourceExpired(error) && !refreshed) refreshed = true
      else { fail(error); return }
      refreshing = renewSource()
      try { await refreshing } catch (freshError) { fail(freshError) }
      finally { refreshing = null }
    }
    player.addEventListener('error', function (event) {
      const expired = sourceExpired(event.detail)
      const incompatible = event.detail.code === shaka.util.Error.Code.CONTENT_UNSUPPORTED_BY_BROWSER && !preferIndexed
      if (expired || incompatible) event.detail.handled = true
      if (expired || incompatible || event.detail.severity === shaka.util.Error.Severity.CRITICAL) handleFailure(event.detail).catch(fail)
    })
    window.addEventListener('pagehide', function (event) {
      if (!event.persisted) player.destroy().catch(function () {})
    })
    const manifestType = root.dataset.streamManifestType === 'hls' ? 'application/x-mpegurl' : 'application/dash+xml'
    await player.load(root.dataset.streamManifest, resumePosition || null, manifestType)
    loaded = true
    renderQualities()
    if (wantsPlay) video.play().catch(function () { wantsPlay = false })
    loadCaptions(JSON.parse(root.dataset.streamTextTracks || '[]') || []).catch(captionFailure)
  } catch (error) { await handleFailure(error) }
}
