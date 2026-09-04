import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

const source = await readFile(new URL('./src/feed/media-row.js', import.meta.url), 'utf8')
const context = vm.createContext({ document: { getElementById: () => null }, matchMedia: () => ({ matches: false }) })
vm.runInContext(source.replaceAll('export function ', 'function '), context)
const target = (...args) => context.mediaRowTarget(...args)

test('overflow smaller than a viewport takes one click regardless of image count', () => {
  assert.equal(target(0, 800, 1400, 1), 600)
  assert.equal(target(600, 800, 1400, -1), 0)
})

test('larger overflow traverses the row in two equal steps', () => {
  assert.equal(target(0, 600, 1208, 1), 304)
  assert.equal(target(304, 600, 1208, 1), 608)
  assert.equal(target(608, 600, 1208, 1), 608)
  assert.equal(target(608, 600, 1208, -1), 304)
  assert.equal(target(304, 600, 1208, -1), 0)
  assert.equal(target(0, 600, 1208, -1), 0)
})

test('wide rows never skip an unseen viewport between stops', () => {
  assert.equal(target(0, 400, 2000, 1), 400)
  assert.equal(target(400, 400, 2000, 1), 800)
  assert.equal(target(0, 900, 800, 1), 0)
})

test('two clicks reach the end even before the first animation finishes', () => {
  const moves = []
  const events = {}
  const viewport = {
    scrollLeft: 0, clientWidth: 600, scrollWidth: 1208,
    querySelectorAll: () => [1, 2, 3],
    addEventListener: (name, fn) => { events[name] = fn },
    scrollTo: ({ left }) => moves.push(left),
  }
  let click
  const next = { dataset: { feedMediaStep: '1' }, addEventListener: (_, fn) => { click = fn } }
  const root = {
    dataset: {}, querySelector: () => viewport, querySelectorAll: () => [next], addEventListener: () => {},
  }
  context.initMediaRows({ querySelectorAll: () => [root] })
  click({ stopPropagation() {} })
  viewport.scrollLeft = 80
  click({ stopPropagation() {} })
  assert.deepEqual(moves, [304, 608])
  events.pointerdown()
  viewport.scrollLeft = 100
  click({ stopPropagation() {} })
  assert.equal(moves.at(-1), 404)
})
