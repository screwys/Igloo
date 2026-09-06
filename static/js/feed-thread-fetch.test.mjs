import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

const source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
const start = source.indexOf('function initThreadAutoFetch(')
const end = source.indexOf('\nfunction closeThreadRoute(', start)

function harness() {
  let finishCapture
  let finishPartial
  const requests = []
  const status = { hidden: true, setAttribute(name, value) { this[name] = value } }
  const list = { children: ['stored'], replaceChildren(...children) { this.children = children } }
  const route = {
    connected: true,
    getAttribute() { return '100' },
    querySelector(selector) {
      return { '[data-thread-fetch-status]': status, '[data-thread-page]': list }[selector] || null
    },
  }
  const context = vm.createContext({
    fetchedThreadRoutes: new WeakSet(),
    window: { location: { pathname: '/thread/100', search: '' } },
    document: { contains(node) { return node.connected } },
    apiFetch(url, opts) {
      requests.push([url, opts.method])
      return new Promise((resolve, reject) => { finishCapture = { resolve, reject } })
    },
    fetch(url) {
      requests.push([url, 'GET'])
      return new Promise(resolve => { finishPartial = () => resolve({ ok: true, text: () => Promise.resolve('fresh') }) })
    },
    partialThreadURL(href) { return href + '?fmt=partial' },
    parseThreadRouteHTML() { return { querySelector() { return { childNodes: ['stored', 'new reply'] } } } },
    initFeedCards() {},
    t(key, fallback) { return fallback },
  })
  vm.runInContext(source.slice(start, end), context)
  return { context, route, list, status, requests, capture: () => finishCapture, partial: () => finishPartial }
}

test('thread opening fetches once and updates the visible stored list without starting another fetch', async () => {
  const h = harness()
  const pending = h.context.initThreadAutoFetch(h.route)
  h.context.initThreadAutoFetch(h.route)
  assert.equal(h.requests.length, 1)
  assert.deepEqual(h.list.children, ['stored'])
  assert.equal(h.status['data-thread-fetch-state'], 'loading')
  h.capture().resolve({ success: true })
  await Promise.resolve()
  h.partial()()
  await pending
  h.context.initThreadAutoFetch(h.route)
  assert.deepEqual(h.requests, [['/api/thread/100/refresh', 'POST'], ['/thread/100?fmt=partial', 'GET']])
  assert.deepEqual(h.list.children, ['stored', 'new reply'])
  assert.equal(h.status.hidden, true)
  h.context.initThreadAutoFetch({ ...h.route })
  assert.equal(h.requests.length, 3, 'a later opening gets its own refresh')
})

test('leaving the thread prevents a completed capture from requesting its fragment', async () => {
  const h = harness()
  const pending = h.context.initThreadAutoFetch(h.route)
  h.context.window.location.pathname = '/feed'
  h.capture().resolve({ success: true })
  await pending
  assert.equal(h.requests.length, 1)
  assert.deepEqual(h.list.children, ['stored'])
})

test('leaving during the fragment fetch prevents stale content replacement', async () => {
  const h = harness()
  const pending = h.context.initThreadAutoFetch(h.route)
  h.capture().resolve({ success: true })
  await Promise.resolve()
  h.route.connected = false
  h.partial()()
  await pending
  assert.deepEqual(h.list.children, ['stored'])
})

test('a failed capture keeps stored content readable and reports a nonblocking status', async () => {
  const h = harness()
  const pending = h.context.initThreadAutoFetch(h.route)
  h.capture().reject(new Error('upstream unavailable'))
  await pending
  assert.deepEqual(h.list.children, ['stored'])
  assert.equal(h.status['data-thread-fetch-state'], 'error')
  assert.equal(h.status.hidden, false)
  assert.match(h.status.textContent, /Could not update/)
})
