import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

class FakeClassList {
  constructor() { this.names = new Set() }
  add(name) { this.names.add(name) }
  remove(name) { this.names.delete(name) }
  contains(name) { return this.names.has(name) }
  toggle(name, enabled) {
    if (enabled) this.names.add(name)
    else this.names.delete(name)
  }
}

class FakeElement {
  constructor() {
    this.attributes = new Map()
    this.dataset = {}
    this.style = {}
    this.hidden = false
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
  removeAttribute(name) { this.attributes.delete(name) }
  click() { this.clicks += 1; this.listeners.get('click')?.() }
  select() {}
  showPopover() {}
  hidePopover() {}
  getBoundingClientRect() { return {left:0,top:100,bottom:140,width:200,height:100} }
  scrollIntoView() { this.scrolls += 1 }
  focus() {}
}

async function loadFilter(labels) {
  const source = await readFile(new URL('./bookmark_filters.js', import.meta.url), 'utf8')
  const root = new FakeElement()
  const modal = new FakeElement()
  const form = new FakeElement()
  const panel = new FakeElement()
  const value = new FakeElement()
  panel.classList.names.add('hidden')
  const toggle = new FakeElement()
  const input = new FakeElement()
  const empty = new FakeElement()
  const reset = new FakeElement()
  const rows = labels.map((label, index) => {
    const row = new FakeElement()
    row.id = `bookmark-filter-result-${index}`
    row.dataset = { name: label, value: label }
    return row
  })
  panel.querySelectorAll = () => rows
  panel.querySelector = () => empty
  root.querySelector = (selector) => ({
    '[role="listbox"]': panel,
    'input[type="hidden"]': value,
    '[data-bookmark-filter-search]': input,
  })[selector] || null
  root.contains = () => true
  form.querySelectorAll = (selector) => selector === '[data-bookmark-filter-field]' ? [root] : []
  form.querySelector = () => reset
  modal.querySelector = () => form
  const document = {
    readyState: 'complete',
    getElementById() { return modal },
    querySelector() { return toggle },
    addEventListener() {},
  }
  vm.runInContext(source, vm.createContext({
    document,
    window: {innerHeight:720,addEventListener() {}},
    MutationObserver: class { observe() {} },
  }), { filename: 'bookmark_filters.js' })
  return { input, rows, value }
}

function keyEvent(key) {
  return { key, prevented: false, preventDefault() { this.prevented = true } }
}

test('filtered bookmark labels can be selected with arrow keys and Enter', async () => {
  const { input, rows, value } = await loadFilter(['Algebra', 'Alpine', 'Beta'])

  input.value = 'al'
  input.dispatch('input')
  input.dispatch('keydown', keyEvent('ArrowDown'))
  input.dispatch('keydown', keyEvent('ArrowUp'))

  assert.equal(rows[1].classList.contains('keyboard-active'), true)
  assert.equal(input.getAttribute('aria-activedescendant'), 'bookmark-filter-result-1')
  assert.equal(rows[1].scrolls, 1)

  const enter = keyEvent('Enter')
  input.dispatch('keydown', enter)
  assert.equal(enter.prevented, true)
  assert.equal(value.value, 'Alpine')
  assert.equal(input.value, 'Alpine')
})
