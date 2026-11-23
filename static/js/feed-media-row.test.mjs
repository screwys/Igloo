import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

const source = await readFile(new URL('./src/feed/media-row.js', import.meta.url), 'utf8')
const context = vm.createContext({
  document: { getElementById: () => null },
  matchMedia: () => ({ matches: false }),
  t: (_, fallback) => fallback,
  materialIconMarkup: () => '',
})
vm.runInContext(source.replace(/^import .*$/m, '').replaceAll('export function ', 'function '), context)

test('small remainders fit through 15 percent and return to scrolling after resize', () => {
  let fitted = false
  const row = {
    style: {
      removeProperty: () => { fitted = false },
      setProperty: name => { if (name === '--feed-row-height') fitted = true },
    },
    getBoundingClientRect: () => ({ width: fitted ? viewport.clientWidth : 1000 }),
  }
  const viewport = {
    clientWidth: 900, scrollLeft: 0,
    get scrollWidth() { return fitted ? this.clientWidth : 1000 },
  }
  const next = { dataset: { feedMediaStep: '1' } }
  const counter = {}
  const fit = {}
  const tiles = [{ offsetLeft: 0, offsetWidth: 500 }, { offsetLeft: 502, offsetWidth: 498 }]
  const controls = {
    '.feed-media-row': row,
    '.feed-media-grid': { children: tiles },
    '[data-feed-media-counter]': counter,
    '[data-feed-media-fit-toggle]': fit,
  }
  const root = {
    hasAttribute: () => false,
    removeAttribute: () => {},
    setAttribute: () => {},
    querySelector: selector => controls[selector] ?? viewport,
    querySelectorAll: () => [next],
  }
  for (const width of [900, 850, 849, 1100, 900]) {
    viewport.clientWidth = width
    context.layoutMediaRow(root)
    assert.equal(viewport.scrollWidth, width >= 850 && width < 1000 ? width : 1000)
    assert.equal(next.hidden, width >= 850)
    assert.equal(counter.hidden, width >= 850)
    assert.equal(fit.hidden, width >= 850)
  }
})
