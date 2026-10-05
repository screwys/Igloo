(function () {
  'use strict';
  var cfg = document.getElementById('temp-dl-config');
  if (!cfg) return;
  var videoID = cfg.dataset.videoId;
  var youtubeURL = cfg.dataset.youtubeUrl;
  var params = new URLSearchParams(location.search);
  var origin = params.get('origin') || 'search';
  var mode = params.get('mode') || window.IglooPlayback.mode(origin);
  var title = document.getElementById('temp-dl-title');
  var status = document.getElementById('temp-dl-status');
  var spinner = document.querySelector('.temp-dl-spinner');
  var actions = document.getElementById('temp-dl-actions');
  var cancel = document.getElementById('temp-dl-cancel-btn');
  var stream = document.getElementById('temp-dl-stream-btn');
  var download = document.getElementById('temp-dl-download-btn');
  var streamController = null;
  var requestID = '';
  var switching = false;
  var finished = false;
  var pollTimer;
  var downloadRequest;
  var csrf = document.querySelector('meta[name="csrf-token"]').content;

  function t(key, fallback) { return (window.IglooI18n.messages || {})[key] || fallback; }
  function show(message, isError) {
    status.textContent = message;
    status.classList.toggle('hidden', !message);
    status.classList.toggle('is-error', !!isError);
  }
  function stop() { clearTimeout(pollTimer); spinner.classList.add('stopped'); }
  function action(button, label, icon) {
    button.title = label;
    button.setAttribute('aria-label', label);
    var glyph = document.querySelector('#material-icon-palette [data-material-icon="' + icon + '"]');
    button.replaceChildren(glyph.cloneNode(true));
  }
  function failed(error, failureTitle) {
    stop();
    title.textContent = failureTitle || cfg.dataset.titleFailed;
    var message = error.message || cfg.dataset.statusFailed;
    show(message === title.textContent ? '' : message, true);
    actions.classList.remove('hidden');
    download.hidden = false;
    switching = false;
    stream.disabled = false;
    cancel.disabled = false;
  }
  async function request(url, body, fallback, signal) {
    var options = body ? { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify(body) } : {};
    if (signal) options.signal = signal;
    var response = await fetch(url, options);
    var raw = await response.text();
    var data;
    try { data = JSON.parse(raw); } catch (_) { throw new Error(fallback || response.statusText || cfg.dataset.statusFailed); }
    if (!response.ok || data.ok === false || data.success === false) throw new Error(data.error_message || data.error || data.message || fallback || response.statusText || cfg.dataset.statusFailed);
    return data;
  }
  function remember(data) { if (data.request_id) requestID = data.request_id; }
  function statusURL() { return cfg.dataset.statusUrl; }
  async function beginStream(cancellation) {
    finished = true;
    clearTimeout(pollTimer);
    spinner.classList.remove('stopped');
    title.textContent = t('stream_preparing_title', 'Preparing stream...');
    show('');
    actions.classList.remove('hidden');
    download.hidden = false;
    stream.disabled = true;
    cancel.dataset.cancelled = '1';
    cancel.disabled = false;
    action(cancel, cfg.dataset.actionBackToVideos, 'ArrowBack');
    var controller = new AbortController();
    streamController = controller;
    try {
      var results = await Promise.all([
        request('/api/youtube/' + encodeURIComponent(videoID) + '/stream', {
          prefer_indexed: !!window.MediaSource && !window.MediaSource.isTypeSupported('audio/mp4; codecs="mp4a.40.2"'),
        }, t('stream_start_failed', 'Could not start streaming.'), controller.signal),
        cancellation,
      ]);
      var data = results[0];
      if (!data.player_url) throw new Error(t('stream_start_failed', 'Could not start streaming.'));
      location.assign(data.player_url);
    } catch (error) {
      if (error.name === 'AbortError') return;
      if (cancellation) finished = false;
      failed(error, t('stream_failed_title', 'Stream failed'));
      action(stream, t('action_retry', 'Retry'), 'Refresh');
      cancel.dataset.cancelled = '1';
      action(cancel, cfg.dataset.actionBackToVideos, 'ArrowBack');
    } finally {
      if (streamController === controller) streamController = null;
    }
  }
  async function pollDownload() {
    if (switching || finished) return;
    try {
      var data = await request(statusURL());
      if (switching || finished) return;
      remember(data);
      if (data.complete) { finished = true; location.assign('/temp/watch?v=' + encodeURIComponent(videoID)); return; }
      if (data.status === 'blocked' || data.status === 'failed') throw new Error(data.error || cfg.dataset.statusFailed);
      if (data.status === 'cancelled') { showCancelled(); return; }
    } catch (error) { failed(error); return; }
    pollTimer = setTimeout(pollDownload, 2000);
  }
  function showCancelled() {
    finished = true;
    stop();
    title.textContent = cfg.dataset.titleCancelled;
    show('');
    cancel.disabled = false;
    cancel.dataset.cancelled = '1';
    action(cancel, cfg.dataset.actionBackToVideos, 'ArrowBack');
    download.hidden = false;
    stream.disabled = false;
  }
  async function cancelDownload(andStream) {
    switching = true;
    clearTimeout(pollTimer);
    cancel.disabled = true;
    stream.disabled = true;
    show(t('temp_download_stopping', 'Stopping download...'));
    try {
      // The queue response supplies the identity of the producer to stop.
      if (downloadRequest) await downloadRequest;
      var cancellation = request('/api/cancel-download', { url: youtubeURL, request_id: requestID }, t('temp_download_stop_failed', 'Could not stop the download.')).then(function (data) {
        if (data.status !== 'cancelled' && data.status !== 'cancelling' && data.status !== 'complete') {
          throw new Error(data.error || t('temp_download_stop_failed', 'Could not stop the download.'));
        }
        return data;
      });
      if (andStream) { await beginStream(cancellation); return; }
      var data = await cancellation;
      while (data.status === 'cancelling') {
        await new Promise(function (resolve) { setTimeout(resolve, 500); });
        data = await request(statusURL());
        if (requestID && data.request_id && requestID !== data.request_id) throw new Error(t('temp_download_changed', 'The download changed. Open the video again.'));
        if (data.complete) data.status = 'complete';
      }
      if (data.status !== 'cancelled' && data.status !== 'complete') throw new Error(data.error || t('temp_download_stop_failed', 'Could not stop the download.'));
      showCancelled();
    } catch (error) { failed(error); }
  }
  stream.addEventListener('click', function () {
    if (finished) beginStream();
    else cancelDownload(true);
  });
  cancel.addEventListener('click', function () {
    if (cancel.dataset.cancelled === '1') {
      if (streamController) streamController.abort();
      location.assign('/videos');
    }
    else cancelDownload(false);
  });
  download.addEventListener('click', function () {
    if (streamController) streamController.abort();
    location.assign('/temp/watch?v=' + encodeURIComponent(videoID) + '&mode=download');
  });
  if (mode === 'stream') { beginStream(); return; }
  downloadRequest = request('/api/quick-download', { url: youtubeURL }).then(function (data) {
    remember(data);
    if (switching || finished) return;
    if (data.queued) pollDownload();
    else if (data.video_id) { finished = true; location.assign('/player/' + encodeURIComponent(data.video_id)); }
    else throw new Error(data.message || cfg.dataset.statusFailed);
  }).catch(function (error) { if (!switching) failed(error); });
})();
