(function () {
  function init() {
    var modal = document.getElementById('bookmark-filters');
    if (!modal) return;
    var toggle = document.querySelector('[data-bookmark-filters-open]');
    var form = modal.querySelector('form');
    var plainInputs = Array.from(form.querySelectorAll('[data-bookmark-search-value]'));
    var from = plainInputs.find(function (input) { return input.name === 'saved_from'; });
    var through = plainInputs.find(function (input) { return input.name === 'saved_through'; });
    var sortInputs = Array.from(form.querySelectorAll('input[name="sort"]'));
    function syncDates() {
      if (from && through) { from.max = through.value; through.min = from.value; }
    }
    plainInputs.forEach(function (input) { input.addEventListener('input', syncDates); });
    var fields = Array.from(form.querySelectorAll('[data-bookmark-filter-field]')).map(function (root) {
      var input = root.querySelector('[data-bookmark-filter-search]');
      var value = root.querySelector('input[type="hidden"]');
      var list = root.querySelector('[role="listbox"]');
      var rows = Array.from(list.querySelectorAll('[role="option"]'));
      var empty = list.querySelector('.bookmark-filter-empty');
      var active = -1;
      var selectedName = input.value;
      var initialValue = value.value;
      var initialName = input.value;

      function visibleRows() { return rows.filter(function (row) { return !row.hidden; }); }
      function close() {
        list.hidePopover();
        list.classList.add('hidden');
        input.setAttribute('aria-expanded', 'false');
        input.removeAttribute('aria-activedescendant');
        input.value = selectedName;
      }
      function open(query) {
        var q = query.trim().toLowerCase();
        rows.forEach(function (row) {
          row.hidden = q !== '' && !row.dataset.name.toLowerCase().includes(q);
          row.classList.remove('keyboard-active');
        });
        empty.classList.toggle('hidden', visibleRows().length > 0);
        active = -1;
        list.classList.remove('hidden');
        var anchor = input.getBoundingClientRect();
        var below = window.innerHeight - anchor.bottom - 8;
        var above = anchor.top - 8;
        list.style.width = anchor.width + 'px';
        list.style.maxHeight = Math.min(220, Math.max(below, above)) + 'px';
        list.showPopover();
        var height = list.getBoundingClientRect().height;
        list.style.left = anchor.left + 'px';
        list.style.top = (below >= height ? anchor.bottom + 4 : anchor.top - height - 4) + 'px';
        input.setAttribute('aria-expanded', 'true');
        input.removeAttribute('aria-activedescendant');
      }
      function select(row) {
        value.value = row.dataset.value;
        selectedName = row.dataset.name;
        rows.forEach(function (option) { option.setAttribute('aria-selected', String(option === row)); });
        close();
      }
      rows.forEach(function (row, index) {
        if (!row.id) row.id = input.id + '-option-' + index;
        // Keep focus in the text field until the selected value is copied.
        row.addEventListener('mousedown', function (event) { event.preventDefault(); });
        row.addEventListener('click', function () { select(row); input.focus(); close(); });
      });
      input.addEventListener('focus', function () { open(''); input.select(); });
      input.addEventListener('click', function () { if (list.classList.contains('hidden')) open(''); });
      input.addEventListener('input', function () {
        if (!input.value) {
          value.value = '';
          selectedName = '';
        }
        open(input.value);
      });
      root.addEventListener('focusout', function (event) {
        if (!root.contains(event.relatedTarget)) close();
      });
      input.addEventListener('keydown', function (event) {
        if (event.key === 'Escape' && !list.classList.contains('hidden')) {
          event.preventDefault();
          event.stopPropagation();
          close();
          return;
        }
        if (!['ArrowDown', 'ArrowUp', 'Enter'].includes(event.key)) return;
        if (event.key === 'Enter') {
          if (list.classList.contains('hidden')) return;
          event.preventDefault();
          var matches = visibleRows();
          if (active >= 0) select(matches[active]);
          else if (matches.length === 1) select(matches[0]);
          return;
        }
        event.preventDefault();
        if (list.classList.contains('hidden')) open('');
        var visible = visibleRows();
        if (!visible.length) return;
        active = active < 0 ? (event.key === 'ArrowDown' ? 0 : visible.length - 1)
          : (active + (event.key === 'ArrowDown' ? 1 : -1) + visible.length) % visible.length;
        rows.forEach(function (row) { row.classList.toggle('keyboard-active', row === visible[active]); });
        input.setAttribute('aria-activedescendant', visible[active].id);
        visible[active].scrollIntoView({ block: 'nearest' });
      });
      return {
        root: root,
        list: list,
        close: close,
        reset: function () { select(rows[0]); },
        restore: function () {
          value.value = initialValue;
          selectedName = initialName;
          rows.forEach(function (row) { row.setAttribute('aria-selected', String(row.dataset.value === initialValue)); });
          close();
        },
      };
    });
    toggle.addEventListener('click', function () {
      plainInputs.forEach(function (input) { input.value = input.defaultValue; });
      sortInputs.forEach(function (input) { input.checked = input.defaultChecked; });
      syncDates();
      fields.forEach(function (field) { field.restore(); });
      window.MpaSiteBase.openModal(modal);
      modal.querySelector('.modal-close').focus();
    });
    form.querySelector('[data-bookmark-filters-reset]').addEventListener('click', function () {
      plainInputs.forEach(function (input) { input.value = ''; });
      sortInputs.forEach(function (input) { input.checked = input.value === ''; });
      syncDates();
      fields.forEach(function (field) { field.reset(); });
    });
    form.addEventListener('submit', function (event) {
      event.preventDefault();
      var values = new FormData(form);
      var params = new URLSearchParams();
      ['category_id', 'posted_by', 'tagged', 'q', 'sort', 'saved_from', 'saved_through'].forEach(function (key) {
        var value = values.get(key);
        if (value && value !== '0') params.set(key, value);
      });
      if (params.has('saved_from') || params.has('saved_through')) params.set('tz', Intl.DateTimeFormat().resolvedOptions().timeZone);
      var label = values.get('label_choice');
      if (label === 'empty') params.set('label_empty', '1');
      else if (label.startsWith('label:')) params.set('label', label.slice(6));
      window.location.assign('/bookmarks' + (params.size ? '?' + params.toString() : ''));
    });
    document.addEventListener('pointerdown', function (event) {
      fields.forEach(function (field) { if (!field.root.contains(event.target)) field.close(); });
    });
    document.addEventListener('scroll', function (event) {
      fields.forEach(function (field) { if (!field.list.contains(event.target)) field.close(); });
    }, true);
    window.addEventListener('resize', function () { fields.forEach(function (field) { field.close(); }); });
    modal.addEventListener('keydown', function (event) {
      if (event.key !== 'Tab') return;
      var controls = Array.from(modal.querySelectorAll('button, input:not([type="hidden"])')).filter(function (el) {
        return !el.hidden && el.getClientRects().length > 0;
      });
      var first = controls[0], last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    });
    new MutationObserver(function () {
      if (modal.classList.contains('hidden')) {
        fields.forEach(function (field) { field.close(); });
        toggle.focus();
      }
    }).observe(modal, { attributes: true, attributeFilter: ['class'] });
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init, { once: true });
  else init();
})();
