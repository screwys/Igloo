(function () {
  'use strict';
  var KEY = 'igloo.playback.v1';
  var profiles = {
    quick: { startup: 0.25, refill: 1, ahead: 12 },
    balanced: { startup: 1, refill: 2, ahead: 30 },
    smooth: { startup: 3, refill: 5, ahead: 60 }
  };

  function validBuffer(value) {
    return value && ['startup', 'refill', 'ahead'].every(function (key) {
      return Number.isFinite(value[key]) && value[key] >= 0;
    }) && value.refill >= value.startup && value.ahead > value.refill;
  }

  function read() {
    var stored = {};
    try { stored = JSON.parse(localStorage.getItem(KEY) || '{}') || {}; } catch (_) {}
    var result = { modes: {}, cap: 720, buffers: {} };
    ['search', 'discover', 'related'].forEach(function (origin) {
      result.modes[origin] = stored.modes && stored.modes[origin] === 'download' ? 'download' : 'stream';
    });
    if (Number.isFinite(stored.cap) && stored.cap >= 0) result.cap = stored.cap;
    ['youtube'].forEach(function (origin) {
      var buffer = stored.buffers && stored.buffers[origin];
      var profile = buffer && buffer.profile;
      if (!profiles[profile] && profile !== 'custom') profile = 'balanced';
      if (profile === 'custom' && !validBuffer(buffer)) profile = 'balanced';
      var values = profile === 'custom' && validBuffer(buffer) ? buffer : profiles[profile] || profiles.balanced;
      result.buffers[origin] = Object.assign({ profile: profile }, values);
    });
    return result;
  }

  function mode(origin) { return read().modes[origin] || 'stream'; }
  function buffer(origin) { return read().buffers[origin]; }
  function text(key, fallback) {
    return (window.IglooI18n && window.IglooI18n.messages && window.IglooI18n.messages[key]) || fallback;
  }

  function updateLinks(root) {
    (root || document).querySelectorAll('a[data-youtube-origin]').forEach(function (link) {
      var origin = link.closest('#player-discovery-rail') ? 'related' : link.dataset.youtubeOrigin;
      var url = new URL(link.href, location.href);
      if (url.pathname !== '/temp/watch') return;
      url.searchParams.set('origin', origin);
      url.searchParams.set('mode', mode(origin));
      var title = link.querySelector('.video-title');
      var thumbnail = link.querySelector('.video-thumbnail img');
      if (title) url.searchParams.set('title', title.textContent.trim());
      if (thumbnail) url.searchParams.set('thumbnail', thumbnail.getAttribute('src'));
      link.href = url.pathname + url.search;
    });
  }

  function renderForm(section, prefs) {
    section.querySelectorAll('[data-playback-mode]').forEach(function (field) {
      field.value = prefs.modes[field.dataset.playbackMode];
      field.dispatchEvent(new Event('change'));
    });
    var cap = section.querySelector('[data-playback-cap]');
    if (cap) { cap.value = String(prefs.cap); cap.dispatchEvent(new Event('change')); }
    section.querySelectorAll('[data-buffer-origin]').forEach(function (group) {
      var value = prefs.buffers[group.dataset.bufferOrigin];
      var profile = group.querySelector('[data-buffer-profile]');
      profile.value = value.profile;
      profile.dispatchEvent(new Event('change'));
      group.querySelector('[data-buffer-custom]').classList.toggle('hidden', value.profile !== 'custom');
      group.querySelectorAll('[data-buffer-value]').forEach(function (field) {
        field.value = String(value[field.dataset.bufferValue]);
        field.disabled = value.profile !== 'custom';
      });
    });
  }

  function init(root) {
    updateLinks(root);
    (root || document).querySelectorAll('[data-browser-playback]').forEach(function (section) {
      if (section.dataset.ready) return;
      section.dataset.ready = '1';
      renderForm(section, read());
      section.addEventListener('change', function (event) {
        var prefs = read();
        var field = event.target;
        var group = field.closest('[data-buffer-origin]');
        if (group && field.hasAttribute('data-buffer-profile') && profiles[field.value]) {
          prefs.buffers[group.dataset.bufferOrigin] = Object.assign({ profile: field.value }, profiles[field.value]);
          renderForm(section, prefs);
        }
        var valid = true;
        section.querySelectorAll('[data-playback-mode]').forEach(function (input) {
          prefs.modes[input.dataset.playbackMode] = input.value;
        });
        prefs.cap = Number(section.querySelector('[data-playback-cap]').value);
        section.querySelectorAll('[data-buffer-origin]').forEach(function (element) {
          var values = { profile: element.querySelector('[data-buffer-profile]').value, ahead: prefs.buffers[element.dataset.bufferOrigin].ahead };
          element.querySelectorAll('[data-buffer-value]').forEach(function (input) {
            if (!input.value.trim() || !input.checkValidity()) valid = false;
            values[input.dataset.bufferValue] = Number(input.value);
          });
          if (!validBuffer(values)) valid = false;
          prefs.buffers[element.dataset.bufferOrigin] = values;
          element.querySelector('[data-buffer-custom]').classList.toggle('hidden', values.profile !== 'custom');
          element.querySelectorAll('[data-buffer-value]').forEach(function (input) {
            input.disabled = values.profile !== 'custom';
          });
        });
        var status = section.querySelector('[data-playback-status]');
        if (!valid) {
          status.textContent = text('settings_buffer_invalid', 'Check startup, refill, and buffer ahead.');
          return;
        }
        try {
          localStorage.setItem(KEY, JSON.stringify(prefs));
          status.textContent = text('settings_browser_saved', 'Saved on this browser.');
          updateLinks(document);
          window.dispatchEvent(new CustomEvent('iglooplaybackpreferenceschange'));
        } catch (_) {
          status.textContent = text('settings_browser_save_failed', 'This browser could not save playback preferences.');
        }
      });
    });
  }

  window.IglooPlayback = { read: read, mode: mode, buffer: buffer };
  init(document);
  document.addEventListener('htmx:afterSettle', function () { init(document); });
  window.addEventListener('storage', function (event) {
    if (event.key === KEY) { updateLinks(document); window.dispatchEvent(new CustomEvent('iglooplaybackpreferenceschange')); }
  });
})();
