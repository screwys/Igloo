(function () {
  'use strict';

  var page = document.getElementById('home-page');
  if (!page) return;
  var widgets = document.getElementById('home-widgets');
  var drawer = document.getElementById('home-drawer');
  var form = drawer.querySelector('[data-home-settings]');
  var catalog = drawer.querySelector('[data-home-catalog]');
  var title = document.getElementById('home-drawer-title');
  var saved = JSON.parse(page.dataset.layout);
  var draft = clone(saved);
  var defaults = JSON.parse(page.dataset.defaultLayout);
  var kinds = JSON.parse(page.dataset.widgetDefaults);
  var editing = false;
  var busy = false;
  var selectedID = '';
  var previewTimer = 0;
  var previewAbort = null;
  var previewVersion = 0;
  var previewIDs = new Set();
  var draggedID = '';
  var dragHandle = null;
  var dragPointer = null;
  var csrf = (document.querySelector('meta[name="csrf-token"]') || {}).content || '';

  function clone(value) { return JSON.parse(JSON.stringify(value)); }
  function t(key, fallback) { return (window.IglooI18n.messages || {})[key] || fallback; }
  function toast(key, fallback) { window.MpaSiteBase.showToast(t(key, fallback)); }
  function widgetByID(id) { return draft.widgets.find(function (widget) { return widget.id === id; }); }
  function newID() { return window.crypto.randomUUID(); }
  function isDirty() { return JSON.stringify(draft) !== JSON.stringify(saved); }

  function placeWidget(section) {
    var index = draft.widgets.findIndex(function (widget) { return widget.id === section.dataset.homeWidget; });
    var following = draft.widgets.slice(index + 1).map(function (widget) {
      return Array.from(widgets.children).find(function (node) { return node.dataset.homeWidget === widget.id; });
    }).find(function (node) { return !!node; });
    widgets.insertBefore(section, following || null);
  }

  function spanFor(size) {
    var columns = draft.columns * 4;
    if (size === 'full') return columns;
    if (size === 'large') return draft.columns === 2 ? columns : Math.max(4, Math.ceil(columns * 2 / 3));
    if (size === 'medium') return Math.max(4, Math.floor(columns / 2));
    return 4;
  }

  function syncLayout() {
    page.classList.toggle('home-editing', editing);
    page.querySelector('[data-home-action="customize"]').setAttribute('aria-pressed', String(editing));
    page.querySelector('[data-home-layout="columns"]').value = draft.columns;
    page.querySelector('[data-home-layout="spacing"]').value = draft.spacing;
    widgets.style.setProperty('--home-columns', draft.columns * 4);
    widgets.style.setProperty('--home-tablet-columns', Math.min(draft.columns * 4, 8));
    widgets.classList.toggle('home-compact', draft.spacing === 'compact');
    draft.widgets.forEach(function (widget, index) {
      var section = Array.from(widgets.children).find(function (node) { return node.dataset.homeWidget === widget.id; });
      if (!section) return;
      section.style.setProperty('--home-span', spanFor(widget.size));
      ['small', 'medium', 'large', 'full'].forEach(function (size) { section.classList.toggle('home-size-' + size, size === widget.size); });
      section.querySelector('[data-home-action="up"]').disabled = busy || index === 0;
      section.querySelector('[data-home-action="down"]').disabled = busy || index === draft.widgets.length - 1;
    });
  }

  function setBusy(value) {
    busy = value;
    page.querySelectorAll('[data-home-action], [data-home-add], [data-home-layout], [data-home-settings] input, [data-home-settings] select').forEach(function (input) { input.disabled = value; });
    syncLayout();
  }

  var sizes = new ResizeObserver(function (entries) {
    var gap = parseFloat(getComputedStyle(widgets).rowGap) || 22;
    entries.forEach(function (entry) {
      var height = entry.target.getBoundingClientRect().height;
      entry.target.parentElement.style.gridRowEnd = 'span ' + Math.max(1, Math.ceil((height + gap) / (8 + gap)));
    });
  });

  function prepareWidgets() {
    sizes.disconnect();
    widgets.querySelectorAll('.home-widget-inner').forEach(function (surface) { sizes.observe(surface); });
    if (window.htmx) window.htmx.process(widgets);
    if (window.FeedTextClamp) window.FeedTextClamp.init(widgets);
    if (window.FeedDates) window.FeedDates.init(widgets);
    if (window.MpaShortsMode) window.MpaShortsMode.refreshSource();
    document.dispatchEvent(new CustomEvent('mpa:infinite-append', { detail: { container: widgets } }));
    syncLayout();
    if (busy) setBusy(true);
  }

  async function renderedLayout(layout, signal) {
    var response = await fetch('/api/home/widgets', {
      method: 'POST', credentials: 'same-origin', signal: signal,
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify(layout)
    });
    if (!response.ok) throw new Error('Home preview failed');
    return response.text();
  }

  function abortPreview() {
    window.clearTimeout(previewTimer);
    previewTimer = 0;
    if (previewAbort) previewAbort.abort();
    previewAbort = null;
    previewVersion++;
  }

  function preview(id) {
    if (id) previewIDs.add(id);
    else previewIDs = new Set(draft.widgets.map(function (widget) { return widget.id; }));
    abortPreview();
    var version = previewVersion;
    previewTimer = window.setTimeout(async function () {
      previewTimer = 0;
      var controller = new AbortController();
      previewAbort = controller;
      try {
        var requested = clone(draft);
        requested.widgets = requested.widgets.filter(function (widget) { return previewIDs.has(widget.id); });
        if (!requested.widgets.length) {
          widgets.replaceChildren();
          prepareWidgets();
          return;
        }
        var html = await renderedLayout(requested, controller.signal);
        if (version !== previewVersion) return;
        var template = document.createElement('template');
        template.innerHTML = html;
        requested.widgets.forEach(function (widget) {
          previewIDs.delete(widget.id);
          if (!widgetByID(widget.id)) return;
          var section = Array.from(template.content.children).find(function (node) { return node.dataset.homeWidget === widget.id; });
          var previous = Array.from(widgets.children).find(function (node) { return node.dataset.homeWidget === widget.id; });
          if (previous) previous.replaceWith(section);
          else placeWidget(section);
        });
        Array.from(widgets.children).forEach(function (section) {
          if (!widgetByID(section.dataset.homeWidget)) section.remove();
        });
        widgets.querySelectorAll('.home-empty').forEach(function (node) { node.remove(); });
        prepareWidgets();
      } catch (error) {
        if (error.name !== 'AbortError') toast('home_preview_failed', 'Could not update preview');
      } finally {
        if (previewAbort === controller) previewAbort = null;
      }
    }, 180);
  }

  function startEditing() {
    if (editing) return;
    editing = true;
    syncLayout();
  }

  function closeDrawer() { if (drawer.open) drawer.close(); }
  function showDrawer() { if (!drawer.open) drawer.showModal(); }

  function showCatalog() {
    startEditing();
    selectedID = '';
    catalog.hidden = false;
    form.hidden = true;
    title.textContent = t('home_add_widget', 'Add widget');
    showDrawer();
  }

  function showSettings(id) {
    var widget = widgetByID(id);
    if (!widget) return;
    startEditing();
    selectedID = id;
    catalog.hidden = true;
    form.hidden = false;
    title.textContent = t('home_widget_settings', 'Widget settings');
    form.reset();
    form.querySelectorAll('[name]').forEach(function (input) {
      var value = widget[input.name];
      if (input.type === 'checkbox') input.checked = Array.isArray(value) ? value.indexOf(input.value) >= 0 : !!value;
      else input.value = value == null ? '' : value;
    });
    var layouts = {
      continue: ['feature', 'cards', 'list'], live: ['feature', 'cards', 'list'],
      starred: ['editorial', 'cards', 'list', 'lanes'], moments: ['portraits', 'cards', 'list'],
      saved: ['cards', 'list'], account: ['editorial', 'cards', 'list'], latest: ['cards', 'list']
    };
    Array.from(form.elements.layout.options).forEach(function (option) {
      option.hidden = layouts[widget.type].indexOf(option.value) < 0;
      option.disabled = option.hidden;
    });
    Array.from(form.elements.order.options).forEach(function (option) {
      if (option.value === 'recent') option.textContent = widget.type === 'saved' ? t('home_recent_saved', 'Recently saved') : t('home_recent', 'Recently watched');
      option.hidden = (option.value === 'recent' && widget.type !== 'continue' && widget.type !== 'saved') || (option.value === 'live' && widget.type !== 'live');
      option.disabled = option.hidden;
    });
    form.querySelector('[data-home-live-states]').hidden = widget.type !== 'live';
    form.querySelector('[data-home-content-types]').hidden = widget.type === 'live' || widget.type === 'continue' || widget.type === 'latest';
    form.querySelector('[data-home-platforms]').hidden = widget.type === 'live';
    form.querySelectorAll('[name="platforms"]').forEach(function (input) {
      input.closest('label').hidden = input.value === 'twitter' && (widget.type === 'continue' || widget.type === 'latest');
    });
    form.querySelector('[data-home-starred-only]').hidden = widget.type === 'starred';
    form.querySelector('[data-home-all="channels"]').closest('label').hidden = widget.type === 'account';
    form.querySelector('[data-home-account-search]').value = '';
    form.querySelectorAll('.home-account-choice').forEach(function (row) { row.hidden = false; });
    syncAllChoices(widget);
    showDrawer();
  }

  function syncAllChoices(widget) {
    form.querySelectorAll('[data-home-all]').forEach(function (input) {
      input.checked = !widget[input.dataset.homeAll] || widget[input.dataset.homeAll].length === 0;
    });
  }

  function moveWidget(id, toIndex) {
    var fromIndex = draft.widgets.findIndex(function (widget) { return widget.id === id; });
    if (fromIndex < 0 || toIndex < 0 || toIndex >= draft.widgets.length || fromIndex === toIndex) return;
    startEditing();
    draft.widgets.splice(toIndex, 0, draft.widgets.splice(fromIndex, 1)[0]);
    var section = Array.from(widgets.children).find(function (node) { return node.dataset.homeWidget === id; });
    if (section) placeWidget(section);
    syncLayout();
  }

  async function cancelEditing() {
    if (busy) return;
    setBusy(true);
    closeDrawer();
    var previous = clone(saved);
    try {
      if (isDirty() && !await window.MpaSiteBase.askConfirm({ title: t('home_discard', 'Discard changes?'), confirmLabel: t('home_discard_action', 'Discard'), danger: false })) return;
      if (!isDirty()) {
        editing = false;
        syncLayout();
        return;
      }
      abortPreview();
      var html = await renderedLayout(previous);
      draft = previous;
      previewIDs.clear();
      editing = false;
      widgets.innerHTML = html;
      prepareWidgets();
    } catch (error) {
      toast('home_preview_failed', 'Could not update preview');
    } finally {
      setBusy(false);
    }
  }

  async function saveLayout() {
    if (busy) return;
    var requested = clone(draft);
    closeDrawer();
    setBusy(true);
    try {
      await window.MpaSiteBase.apiJson('/api/home/layout', { method: 'PUT', body: JSON.stringify(requested) });
      saved = requested;
      editing = false;
      syncLayout();
      if (previewIDs.size && !previewTimer && !previewAbort) preview(Array.from(previewIDs)[0]);
      toast('home_saved_layout', 'Layout saved');
    } catch (error) {
      toast('home_save_failed', 'Could not save layout');
    } finally {
      setBusy(false);
    }
  }

  async function removeWidget(id) {
    setBusy(true);
    try {
      if (!await window.MpaSiteBase.askConfirm({ title: t('home_remove_confirm', 'Remove widget?'), confirmLabel: t('action_remove', 'Remove'), danger: true })) return;
      var requested = clone(saved);
      requested.widgets = requested.widgets.filter(function (widget) { return widget.id !== id; });
      if (requested.widgets.length !== saved.widgets.length) {
        await window.MpaSiteBase.apiJson('/api/home/layout', { method: 'PUT', body: JSON.stringify(requested) });
        saved = requested;
      }
      draft.widgets = draft.widgets.filter(function (widget) { return widget.id !== id; });
      previewIDs.delete(id);
      var section = Array.from(widgets.children).find(function (node) { return node.dataset.homeWidget === id; });
      if (section) {
        sizes.unobserve(section.querySelector('.home-widget-inner'));
        section.remove();
      }
      if (window.MpaShortsMode) window.MpaShortsMode.refreshSource();
      syncLayout();
    } catch (error) {
      toast('home_save_failed', 'Could not save layout');
    } finally {
      setBusy(false);
    }
  }

  page.addEventListener('click', async function (event) {
    if (busy) return;
    var add = event.target.closest('[data-home-add]');
    if (add) {
      var base = kinds.find(function (widget) { return widget.type === add.dataset.homeAdd; });
      var widget = clone(base);
      widget.id = newID();
      draft.widgets.push(widget);
      preview(widget.id);
      showSettings(widget.id);
      return;
    }
    var button = event.target.closest('[data-home-action]');
    if (!button) return;
    var action = button.dataset.homeAction;
    var section = button.closest('[data-home-widget]');
    var id = section && section.dataset.homeWidget;
    var widget = widgetByID(id);
    if (action === 'add') showCatalog();
    if (action === 'customize') { if (editing) await cancelEditing(); else startEditing(); }
    if (action === 'cancel') await cancelEditing();
    if (action === 'save') await saveLayout();
    if (action === 'close-drawer') closeDrawer();
    if (action === 'configure') showSettings(id);
    if (action === 'up' || action === 'down') moveWidget(id, draft.widgets.indexOf(widget) + (action === 'up' ? -1 : 1));
    if (action === 'resize' && widget) {
      var choices = ['small', 'medium', 'large', 'full'];
      widget.size = choices[(choices.indexOf(widget.size) + 1) % choices.length];
      syncLayout();
    }
    if (action === 'duplicate' && widget) {
      var copy = clone(widget);
      copy.id = newID();
      draft.widgets.splice(draft.widgets.indexOf(widget) + 1, 0, copy);
      preview(copy.id);
    }
    if (action === 'remove' && widget) await removeWidget(id);
    if (action === 'reset' && await window.MpaSiteBase.askConfirm({ title: t('home_reset_confirm', 'Reset layout?'), confirmLabel: t('action_reset', 'Reset'), danger: true })) {
      draft = clone(defaults);
      draft.widgets.slice().reverse().forEach(function (widget) {
        var section = Array.from(widgets.children).find(function (node) { return node.dataset.homeWidget === widget.id; });
        if (section) placeWidget(section);
      });
      preview();
      syncLayout();
    }
  });

  page.addEventListener('change', function (event) {
    if (busy) return;
    var input = event.target;
    if (input.dataset.homeLayout) {
      draft[input.dataset.homeLayout] = input.dataset.homeLayout === 'columns' ? Number(input.value) : input.value;
      syncLayout();
    }
  });

  form.addEventListener('submit', function (event) { event.preventDefault(); closeDrawer(); });
  form.addEventListener('input', function (event) {
    if (busy) return;
    var input = event.target;
    if (input.hasAttribute('data-home-account-search')) {
      var query = input.value.trim().toLocaleLowerCase();
      form.querySelectorAll('.home-account-choice').forEach(function (row) { row.hidden = row.dataset.homeChannelSearch.toLocaleLowerCase().indexOf(query) < 0; });
      return;
    }
    var widget = widgetByID(selectedID);
    if (!widget) return;
    if (input.dataset.homeAll) {
      widget[input.dataset.homeAll] = [];
      form.querySelectorAll('[name="' + input.dataset.homeAll + '"]').forEach(function (choice) { choice.checked = false; });
      input.checked = true;
    } else if (input.type === 'checkbox') {
      if (['platforms', 'channels', 'content_types', 'live_states'].indexOf(input.name) >= 0) {
        widget[input.name] = Array.from(form.querySelectorAll('[name="' + input.name + '"]:checked')).map(function (choice) { return choice.value; });
        syncAllChoices(widget);
      } else widget[input.name] = input.checked;
    } else if (input.name === 'count') {
      if (!input.value || !input.checkValidity()) return;
      widget.count = Number(input.value);
    } else widget[input.name] = input.value;
    preview(widget.id);
  });

  page.addEventListener('pointerdown', function (event) {
    var handle = event.target.closest('[data-home-action="drag"]');
    if (!editing || busy || !handle || event.button !== 0) return;
    event.preventDefault();
    draggedID = handle.closest('[data-home-widget]').dataset.homeWidget;
    dragHandle = handle;
    dragPointer = event.pointerId;
    handle.setPointerCapture(event.pointerId);
    handle.closest('[data-home-widget]').classList.add('home-dragging');
  });
  page.addEventListener('pointermove', function (event) {
    if (!draggedID || event.pointerId !== dragPointer) return;
    var target = document.elementFromPoint(event.clientX, event.clientY);
    var section = target && target.closest('[data-home-widget]');
    widgets.querySelectorAll('.home-drop-target').forEach(function (node) { node.classList.remove('home-drop-target'); });
    if (section && page.contains(section) && section.dataset.homeWidget !== draggedID) section.classList.add('home-drop-target');
  });
  function finishDrag(event) {
    if (!draggedID || event.pointerId !== dragPointer) return;
    var target = document.elementFromPoint(event.clientX, event.clientY);
    var section = target && target.closest('[data-home-widget]');
    var id = draggedID;
    var handle = dragHandle;
    draggedID = '';
    dragHandle = null;
    dragPointer = null;
    widgets.querySelectorAll('.home-drop-target, .home-dragging').forEach(function (node) { node.classList.remove('home-drop-target', 'home-dragging'); });
    if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
    if (!busy && event.type === 'pointerup' && section && page.contains(section)) moveWidget(id, draft.widgets.findIndex(function (widget) { return widget.id === section.dataset.homeWidget; }));
  }
  page.addEventListener('pointerup', finishDrag);
  page.addEventListener('pointercancel', finishDrag);
  page.addEventListener('lostpointercapture', finishDrag);
  drawer.addEventListener('click', function (event) { if (event.target === drawer) closeDrawer(); });
  window.addEventListener('beforeunload', function (event) { if (editing && isDirty()) { event.preventDefault(); event.returnValue = ''; } });
  prepareWidgets();
})();
