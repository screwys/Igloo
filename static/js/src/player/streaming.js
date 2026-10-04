import { apiFetch, showToast, t } from '../utils.js'
import { playVideo } from './playback.js'
import { configureXSpaceAudio } from './x-space-audio.js'

export async function initStreaming(video, root, autoplay, resumePosition) {
  const quality = document.getElementById('player-quality-menu-btn')
  const qualityMenu = document.getElementById('player-quality-menu')
  const qualityWrap = document.getElementById('player-quality-menu-wrap')
  const download = document.getElementById('player-stream-download-btn')
	const refresh = document.getElementById('player-stream-refresh-btn')
  const controller = root.querySelector('media-controller')
  const liveButton = root.querySelector('media-live-button')
  const timeRange = root.querySelector('media-time-range')
  const timeDisplay = root.querySelector('media-time-display')
  let loaded = false
  let wantsPlay = autoplay
  let started = false
  let manualTrack = null
  let player
  let refreshed = false
  let refreshing = null
  let nativePlayback = false
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
    configureXSpaceAudio(player, root.dataset.originalUrl)

    function syncLiveControls() {
      if (!loaded || nativePlayback) return
      const live = player.isLive()
      const range = player.seekRange()
      if (video.streamType !== (live ? 'live' : 'on-demand')) {
        video.streamType = live ? 'live' : 'on-demand'
        video.dispatchEvent(new Event('streamtypechange'))
      }
      controller?.classList.toggle('live-media-controller', live)
      if (liveButton) liveButton.hidden = !live
      if (timeDisplay) {
        timeDisplay.toggleAttribute('remaining', live)
        timeDisplay.toggleAttribute('notoggle', live)
        timeDisplay.toggleAttribute('showduration', !live)
      }
      if (timeRange) {
        timeRange.hidden = live && range.end <= range.start
        // Media Chrome's preview uses duration; a live window has a finite end but infinite duration.
        if (live) {
          timeRange.setAttribute('mediaduration', String(range.end))
          if (!video.paused && controller?.hasAttribute('mediatimeislive')) {
            timeRange.setAttribute('mediacurrenttime', String(range.end))
          }
        }
      }
    }
    video.addEventListener('timeupdate', syncLiveControls)
    video.addEventListener('progress', syncLiveControls)
    video.addEventListener('durationchange', syncLiveControls)

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
      refreshed = false
      if (!started) { started = true; configure() }
    })
    window.addEventListener('iglooplaybackpreferenceschange', configure)
    function renderQualities() {
      if (!quality || !qualityMenu) return
      const current = manualTrack === null ? 'auto' : String(manualTrack)
      const tracks = player.getVariantTracks().slice().sort(function (a, b) { return (b.height - a.height) || (b.bandwidth - a.bandwidth) })
      const options = [{ value: 'auto', label: t('player_quality_auto', 'Auto') }]
      tracks.forEach(function (track) {
        const resolution = track.height ? track.height + 'p' : t('player_quality_audio', 'Audio')
        const rate = track.frameRate > 30 ? ' ' + Math.round(track.frameRate) + 'fps' : ''
        const codec = track.videoCodec ? ' ' + track.videoCodec.split('.')[0] : ''
        const language = track.language && track.language !== 'und' ? ' ' + track.language : ''
        options.push({ value: String(track.id), label: resolution + rate + codec + language })
      })
      const focused = qualityMenu.contains(document.activeElement) ? document.activeElement.dataset.quality : null
      qualityMenu.replaceChildren()
      options.forEach(function (option) {
        const button = document.createElement('button')
        button.type = 'button'
        button.className = 'mc-speed-option'
        button.dataset.quality = option.value
        button.setAttribute('role', 'menuitemradio')
        button.setAttribute('aria-checked', String(option.value === current))
        button.textContent = option.label
        qualityMenu.appendChild(button)
        if (focused === option.value) button.focus()
      })
      const selected = options.find(function (option) { return option.value === current }) || options[0]
      quality.title = t('player_quality', 'Quality') + ' (' + selected.label + ')'
      quality.setAttribute('aria-label', quality.title)
      quality.disabled = !loaded
    }
    if (qualityMenu) qualityMenu.addEventListener('click', function (event) {
      const option = event.target.closest('[data-quality]')
      if (!option) return
      manualTrack = option.dataset.quality === 'auto' ? null : Number(option.dataset.quality)
      qualityMenu.classList.add('hidden')
      quality.setAttribute('aria-expanded', 'false')
      quality.focus()
      configure()
      if (manualTrack !== null) {
        const track = player.getVariantTracks().find(function (candidate) { return candidate.id === manualTrack })
        if (track) player.selectVariantTrack(track, true, window.IglooPlayback.buffer('youtube').refill)
      }
      renderQualities()
    })
    player.addEventListener('trackschanged', renderQualities)
    function loadCaptions(tracks) {
      root.dispatchEvent(new CustomEvent('streamclockready', { detail: { position: function () {
        if (!loaded || nativePlayback || !player.isLive()) return null
        const date = player.getPlayheadTimeAsDate()
        return date && date.getTime()
      } } }))
      player.selectTextTrack(null)
      let requestedCaption = null
      root.dispatchEvent(new CustomEvent('captiontrackschanged', { detail: {
        tracks: tracks,
        selectTrack: async function (caption) {
          requestedCaption = caption
          if (!caption) { player.selectTextTrack(null); return }
          let track = player.getTextTracks().find(function (candidate) {
            return candidate.language === caption.language && candidate.label === caption.label
          })
          if (!track) track = await player.addTextTrackAsync(caption.url, caption.language, 'subtitles', 'text/vtt', undefined, caption.label)
          if (requestedCaption === caption) player.selectTextTrack(track)
        },
      } }))
    }
    async function renewSource() {
      const position = loaded ? video.currentTime : resumePosition || 0
      let rate = video.playbackRate
      const selectedQuality = player.getVariantTracks().find(function (track) { return track.id === manualTrack })
      loaded = false
      if (quality) quality.disabled = true
      if (qualityMenu) qualityMenu.classList.add('hidden')
      if (quality) quality.setAttribute('aria-expanded', 'false')
      const fresh = await apiFetch('/api/youtube/' + encodeURIComponent(root.dataset.videoId) + '/stream', { method: 'POST', body: JSON.stringify({ prefer_indexed: preferIndexed }) })
      if (!fresh || !fresh.success) throw new Error('stream renewal failed')
      if (fresh.media_url) {
        nativePlayback = true
        await player.unload()
        root.dataset.streamManifest = ''
        root.dataset.streamSessionId = ''
        if (qualityWrap) qualityWrap.classList.add('hidden')
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
        video.streamType = 'on-demand'
        video.dispatchEvent(new Event('streamtypechange'))
        controller?.classList.remove('live-media-controller')
        if (liveButton) liveButton.hidden = true
        if (timeRange) timeRange.hidden = false
        if (timeDisplay) {
          timeDisplay.removeAttribute('remaining')
          timeDisplay.removeAttribute('notoggle')
          timeDisplay.setAttribute('showduration', '')
        }
        if (wantsPlay) playVideo(video, () => wantsPlay).catch(function () { wantsPlay = false })
        const response = await apiFetch('/api/videos/' + encodeURIComponent(root.dataset.videoId) + '/subtitles').catch(function () { return { tracks: [] } })
        root.dispatchEvent(new CustomEvent('captiontrackschanged', { detail: {
          refresh: true,
          tracks: (response.tracks || []).map(function (track) {
            return { url: '/api/media/subtitle/' + encodeURIComponent(root.dataset.videoId) + '?track=' + encodeURIComponent(track.track_id),
              language: track.srclang, label: track.label, automatic: !!track.is_auto }
          }),
        } }))
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
      syncLiveControls()
      renderQualities()
      if (wantsPlay) playVideo(video, () => wantsPlay).catch(function () { wantsPlay = false })
      loadCaptions(fresh.text_tracks || [])
    }
    if (refresh) refresh.addEventListener('click', function () {
      if (!loaded || nativePlayback || refreshing) return
      video.currentTime = player.seekRange().end
      wantsPlay = true
      playVideo(video, () => wantsPlay).catch(function () { wantsPlay = false })
    })
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
    syncLiveControls()
    renderQualities()
    if (wantsPlay) playVideo(video, () => wantsPlay).catch(function () { wantsPlay = false })
    loadCaptions(JSON.parse(root.dataset.streamTextTracks || '[]') || [])
  } catch (error) { await handleFailure(error) }
}
