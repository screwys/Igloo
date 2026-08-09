(function () {
  function closeFilter(root) {
    var panel = root.querySelector('[data-bookmark-label-panel]');
    var toggle = root.querySelector('[data-bookmark-label-toggle]');
    var input = root.querySelector('[data-bookmark-label-search]');
    if (panel) panel.classList.add('hidden');
    if (toggle) toggle.setAttribute('aria-expanded', 'false');
    if (input) {
      input.setAttribute('aria-expanded', 'false');
      input.setAttribute('aria-activedescendant', '');
    }
  }

  function openFilter(root) {
    var panel = root.querySelector('[data-bookmark-label-panel]');
    var toggle = root.querySelector('[data-bookmark-label-toggle]');
    var input = root.querySelector('[data-bookmark-label-search]');
    if (panel) panel.classList.remove('hidden');
    if (toggle) toggle.setAttribute('aria-expanded', 'true');
    if (input) input.setAttribute('aria-expanded', 'true');
    if (input) {
      input.value = '';
      filterRows(root, '');
      setTimeout(function () { input.focus(); }, 0);
    }
  }

  function visibleRows(root) {
    return Array.prototype.slice.call(root.querySelectorAll('[data-bookmark-label-row]')).filter(function (row) {
      return !row.classList.contains('hidden');
    });
  }

  function setKeyboardActiveRow(root, activeRow) {
    var input = root.querySelector('[data-bookmark-label-search]');
    var rows = visibleRows(root);
    rows.forEach(function (row) {
      var active = row === activeRow;
      row.classList.toggle('keyboard-active', active);
      row.setAttribute('aria-selected', active ? 'true' : 'false');
    });
    if (input) input.setAttribute('aria-activedescendant', activeRow ? activeRow.id : '');
    if (activeRow) activeRow.scrollIntoView({ block: 'nearest' });
  }

  function filterRows(root, query) {
    var q = String(query || '').trim().toLowerCase();
    var rows = Array.prototype.slice.call(root.querySelectorAll('[data-bookmark-label-row]'));
    var empty = root.querySelector('[data-bookmark-label-empty]');
    var visible = 0;
    rows.forEach(function (row) {
      var label = String(row.getAttribute('data-label') || '').toLowerCase();
      var show = !q || label.indexOf(q) !== -1;
      row.classList.toggle('hidden', !show);
      if (show) visible += 1;
    });
    if (empty) empty.classList.toggle('hidden', visible > 0);
    setKeyboardActiveRow(root, null);
  }

  function init() {
    var filters = Array.prototype.slice.call(document.querySelectorAll('[data-bookmark-label-filter]'));
    filters.forEach(function (root) {
      var toggle = root.querySelector('[data-bookmark-label-toggle]');
      var input = root.querySelector('[data-bookmark-label-search]');
      if (toggle) {
        toggle.addEventListener('click', function (event) {
          event.preventDefault();
          var panel = root.querySelector('[data-bookmark-label-panel]');
          if (panel && panel.classList.contains('hidden')) openFilter(root);
          else closeFilter(root);
        });
      }
      if (input) {
        input.addEventListener('input', function () { filterRows(root, input.value); });
        input.addEventListener('keydown', function (event) {
          if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp' && event.key !== 'Enter') return;
          var rows = visibleRows(root);
          if (!rows.length) return;

          if (event.key === 'Enter') {
            var activeRow = rows.find(function (row) { return row.classList.contains('keyboard-active'); });
            if (!activeRow) return;
            event.preventDefault();
            activeRow.click();
            return;
          }

          event.preventDefault();
          var activeIndex = rows.findIndex(function (row) { return row.classList.contains('keyboard-active'); });
          var nextIndex;
          if (activeIndex === -1) nextIndex = event.key === 'ArrowDown' ? 0 : rows.length - 1;
          else nextIndex = (activeIndex + (event.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length;
          setKeyboardActiveRow(root, rows[nextIndex]);
        });
      }
    });

    document.addEventListener('mousedown', function (event) {
      filters.forEach(function (root) {
        if (!root.contains(event.target)) closeFilter(root);
      });
    });
    document.addEventListener('keydown', function (event) {
      if (event.key !== 'Escape') return;
      filters.forEach(closeFilter);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init, { once: true });
  } else {
    init();
  }
})();
