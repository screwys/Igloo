import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

class FakeClassList {
  constructor() { this.names = new Set() }
  contains(name) { return this.names.has(name) }
  toggle(name, enabled) {
    if (enabled) this.names.add(name)
    else this.names.delete(name)
  }
}

class FakeElement {
  constructor() {
    this.attributes = new Map()
    this.classList = new FakeClassList()
    this.listeners = new Map()
    this.value = ''
    this.clicks = 0
    this.scrolls = 0
  }
  addEventListener(type, listener) { this.listeners.set(type, listener) }
  dispatch(type, event = {}) { this.listeners.get(type)(event) }
  getAttribute(name) { return this.attributes.get(name) || null }
  setAttribute(name, value) { this.attributes.set(name, String(value)) }
  click() { this.clicks += 1 }
  scrollIntoView() { this.scrolls += 1 }
  focus() {}
}

async function loadFilter(labels) {
  const source = await readFile(new URL('./bookmark_label_filter.js', import.meta.url), 'utf8')
  const root = new FakeElement()
  const panel = new FakeElement()
  panel.classList.names.add('hidden')
  const toggle = new FakeElement()
  const input = new FakeElement()
  const empty = new FakeElement()
  const rows = labels.map((label, index) => {
    const row = new FakeElement()
    row.id = `bookmark-label-result-${index}`
    row.setAttribute('data-label', label)
    return row
  })
  root.querySelector = (selector) => ({
    '[data-bookmark-label-panel]': panel,
    '[data-bookmark-label-toggle]': toggle,
    '[data-bookmark-label-search]': input,
    '[data-bookmark-label-empty]': empty,
  })[selector] || null
  root.querySelectorAll = (selector) => selector === '[data-bookmark-label-row]' ? rows : []
  root.contains = () => true

  const document = {
    readyState: 'complete',
    querySelectorAll(selector) { return selector === '[data-bookmark-label-filter]' ? [root] : [] },
    addEventListener() {},
  }
  vm.runInContext(source, vm.createContext({ document, setTimeout: (fn) => fn() }), { filename: 'bookmark_label_filter.js' })
  return { input, rows }
}

function keyEvent(key) {
  return { key, prevented: false, preventDefault() { this.prevented = true } }
}

test('filtered bookmark labels can be selected with arrow keys and Enter', async () => {
  const { input, rows } = await loadFilter(['Algebra', 'Alpine', 'Beta'])

  input.value = 'al'
  input.dispatch('input')
  input.dispatch('keydown', keyEvent('ArrowDown'))
  input.dispatch('keydown', keyEvent('ArrowUp'))

  assert.equal(rows[1].classList.contains('keyboard-active'), true)
  assert.equal(rows[1].getAttribute('aria-selected'), 'true')
  assert.equal(input.getAttribute('aria-activedescendant'), 'bookmark-label-result-1')
  assert.equal(rows[1].scrolls, 1)

  const enter = keyEvent('Enter')
  input.dispatch('keydown', enter)
  assert.equal(enter.prevented, true)
  assert.equal(rows[1].clicks, 1)
})
