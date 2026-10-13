import { apiFetch, setPlaybackRangeProvider, t } from '../utils.js'
import { configureXSpaceAudio } from './x-space-audio.js'

export function createXLivePlayback(video, url, options) {
  let abort = null
  let player = null
  let sessionID = ''
  let cleanup = Promise.resolve()

  function release(id) {
    if (id) apiFetch('/api/streams/' + encodeURIComponent(id), { method: 'DELETE', keepalive: true }).catch(function () {})
  }

  function stop() {
    if (abort) abort.abort()
    abort = null
    video.pause()
    release(sessionID)
    sessionID = ''
    const previous = player
    player = null
    if (previous) cleanup = previous.destroy().catch(function () {})
    return cleanup
  }

  async function refresh() {
    video.defaultPlaybackRate = video.playbackRate
    const stopped = stop()
    const request = new AbortController()
    abort = request
    options.onStatus(t('status_loading', 'Loading...'))
    await stopped
    if (abort !== request) return

    function fail(error) {
      if (abort !== request || request.signal.aborted) return
      console.debug('[Feed] live playback failed', String(error && (error.detail || error).code))
      const payload = error && error.payload
      const message = payload && payload.error_code === 'live_ended'
        ? t('shorts_live_ended', 'Live stream ended')
        : payload && payload.error_message || t('shorts_live_playback_failed', 'Live stream could not play')
      stop()
      options.onStatus(message)
    }

    try {
      const result = await apiFetch('/api/x/stream', { method: 'POST', body: JSON.stringify({ url }), signal: request.signal })
      if (abort !== request) { release(result.session_id); return }
      sessionID = result.session_id
      if (result.live) {
        video.dataset.liveStream = result.live.live_status === 'is_live' ? '1' : '0'
        options.onLive(result.live)
      }
      const shaka = window.shaka
      if (!shaka) throw new Error('stream player unavailable')
      shaka.polyfill.installAll()
      const stream = new shaka.Player()
      player = stream
      let startedAtLiveEdge = false
      function startAtLiveEdge() {
        if (startedAtLiveEdge || video.readyState < 2 || !stream.isLive()) return
        const range = stream.seekRange()
        if (range.end <= range.start) return
        let buffered = false
        for (let i = 0; i < video.buffered.length; i++) {
          if (range.end >= video.buffered.start(i) && range.end < video.buffered.end(i)) buffered = true
        }
        if (!buffered) return
        startedAtLiveEdge = true
        video.removeEventListener('loadeddata', startAtLiveEdge)
        video.removeEventListener('timeupdate', startAtLiveEdge)
        stream.goToLive()
      }
      video.addEventListener('loadeddata', startAtLiveEdge, { signal: request.signal })
      video.addEventListener('timeupdate', startAtLiveEdge, { signal: request.signal })
      stream.addEventListener('manifestupdated', function () {
        if (player === stream) {
          startAtLiveEdge()
          video.dispatchEvent(new Event('progress'))
        }
      })
      stream.addEventListener('error', function (event) {
        if (event.detail.severity === shaka.util.Error.Severity.CRITICAL) fail(event.detail)
      })
      await stream.attach(video)
      configureXSpaceAudio(stream, url)
      if (abort !== request) return
      await stream.load(result.manifest_url, null, result.manifest_type === 'hls' ? 'application/x-mpegurl' : 'application/dash+xml')
      if (abort !== request) return
      startAtLiveEdge()
      video.playbackRate = video.defaultPlaybackRate
      options.onStatus('')
      video.play().catch(function () {})
    } catch (error) {
      if (error.name !== 'AbortError') fail(error)
    }
  }

  video.dataset.liveStream = '1'
  setPlaybackRangeProvider(video, function () { return player ? player.seekRange() : null })
  refresh()
  return { stop, refresh }
}
