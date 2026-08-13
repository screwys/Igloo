import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

class FakeClassList {
  constructor(element) {
    this.element = element
  }

  values() {
    return new Set(String(this.element.className || '').split(/\s+/).filter(Boolean))
  }

  contains(name) {
    return this.values().has(name)
  }

  add(name) {
    const values = this.values()
    values.add(name)
    this.element.className = Array.from(values).join(' ')
  }

  toggle(name, force) {
    const values = this.values()
    const enabled = force === undefined ? !values.has(name) : force
    if (enabled) values.add(name)
    else values.delete(name)
    this.element.className = Array.from(values).join(' ')
    return enabled
  }
}

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName
    this.attributes = new Map()
    this.children = []
    this.dataset = {}
    this.listeners = new Map()
    this.value = ''
    this.classList = new FakeClassList(this)
    this.style = {
      values: new Map(),
      setProperty: (name, value) => this.style.values.set(name, value),
    }
  }

  appendChild(child) {
    this.children.push(child)
    return child
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value))
  }

  getAttribute(name) {
    return this.attributes.get(name) ?? null
  }

  hasAttribute(name) {
    return this.attributes.has(name)
  }

  removeAttribute(name) {
    this.attributes.delete(name)
  }

  addEventListener(name, listener) {
    if (!this.listeners.has(name)) this.listeners.set(name, [])
    this.listeners.get(name).push(listener)
  }

  dispatch(name, details = {}) {
    const event = { preventDefault() {}, stopPropagation() {}, ...details }
    for (const listener of this.listeners.get(name) || []) listener(event)
  }

  querySelector(selector) {
    const match = /^\[([^=\]]+)(?:="([^"]*)")?\]$/.exec(selector)
    if (match && this.attributes.has(match[1]) && (match[2] === undefined || this.attributes.get(match[1]) === match[2])) return this
    if (selector.startsWith('.') && String(this.className || '').split(/\s+/).includes(selector.slice(1))) return this
    for (const child of this.children) {
      const found = child.querySelector(selector)
      if (found) return found
    }
    return null
  }

  querySelectorAll(selector) {
    const matches = []
    const match = /^\[([^=\]]+)(?:="([^"]*)")?\]$/.exec(selector)
    if (match && this.attributes.has(match[1]) && (match[2] === undefined || this.attributes.get(match[1]) === match[2])) matches.push(this)
    if (selector.startsWith('.') && String(this.className || '').split(/\s+/).includes(selector.slice(1))) matches.push(this)
    for (const child of this.children) matches.push(...child.querySelectorAll(selector))
    return matches
  }

  contains(element) {
    if (element === this) return true
    return this.children.some((child) => child.contains(element))
  }
}

class FakeVideo extends FakeElement {
  constructor() {
    super('video')
    this.paused = true
    this.muted = true
    this.volume = 0.8
    this.playbackRate = 1
    this.defaultPlaybackRate = 1
  }

  play() {
    this.paused = false
    this.dispatch('play')
    return Promise.resolve()
  }

  pause() {
    this.paused = true
    this.dispatch('pause')
  }
}

async function loadVideoControls() {
  const source = await readFile(new URL('./src/feed/video-controls.js', import.meta.url), 'utf8')
  const visibilitySource = await readFile(new URL('./src/video-controls-visibility.js', import.meta.url), 'utf8')
  const runnable = "const attachSeekTooltip = () => {}; const makeDraggableSeekbar = () => {}; const setSvgContent = () => {}; const t = (_key, fallback) => fallback;\n" +
    visibilitySource.replace(/\bexport\s+/g, '') + '\n' +
    source.replace(/^import .*$/gm, '').replace(/\bexport\s+/g, '') +
    '\nObject.assign(globalThis, { createFeedVideoControls, bindFeedVideoControls });'

  let pendingTimer = null
  const window = {
    setTimeout(callback) {
      pendingTimer = callback
      return 1
    },
    clearTimeout() {
      pendingTimer = null
    },
  }
  const document = new FakeElement('document')
  document.createElement = (tagName) => new FakeElement(tagName)
  const context = vm.createContext({ document, window, HTMLVideoElement: FakeVideo })
  vm.runInContext(runnable, context, { filename: 'video-controls.js' })
  context.runPendingTimer = function () {
    const callback = pendingTimer
    pendingTimer = null
    if (callback) callback()
  }
  return context
}

test('vertical volume control changes volume without toggling feed playback', async () => {
  const media = await loadVideoControls()
  const wrap = new FakeElement('div')
  const controls = media.createFeedVideoControls()
  const video = new FakeVideo()
  wrap.appendChild(controls)

  media.bindFeedVideoControls(wrap, video)
  const slider = controls.querySelector('[data-feed-video-volume]')
  const volumeControl = controls.querySelector('[data-feed-video-volume-control]')
  const popover = controls.querySelector('.feed-video-volume-popover')

  assert.equal(slider.value, '0')
  slider.value = '0.35'
  slider.dispatch('input')

  assert.equal(video.volume, 0.35)
  assert.equal(video.muted, false)
  assert.equal(video.paused, true)
  assert.equal(popover.style.values.get('--feed-volume-height'), '27px')

  let corridorClickStopped = false
  volumeControl.dispatch('click', { stopPropagation() { corridorClickStopped = true } })
  assert.equal(corridorClickStopped, true)
  assert.equal(video.paused, true)

  const speedButton = controls.querySelector('[data-feed-video-speed-button]')
  const speedMenu = controls.querySelector('[data-feed-video-speed-menu]')
  const speedOption = controls.querySelector('[data-rate="1.5"]')
  assert.equal(speedButton.tagName, 'button')
  assert.match(speedButton.className, /(?:^|\s)feed-video-control-btn(?:\s|$)/)
  assert.equal(speedMenu.getAttribute('role'), 'menu')
  speedButton.dispatch('click')
  assert.equal(speedButton.getAttribute('aria-expanded'), 'true')
  assert.equal(speedMenu.classList.contains('hidden'), false)
  speedOption.dispatch('click')
  assert.equal(video.playbackRate, 1.5)
  assert.equal(video.defaultPlaybackRate, 1.5)
  assert.equal(speedButton.textContent, '1.5x')
  assert.equal(speedOption.getAttribute('aria-checked'), 'true')
  assert.equal(speedMenu.classList.contains('hidden'), true)
  assert.equal(video.paused, true)
})

test('feed controls reuse player pointer and focus auto-hide behavior', async () => {
  const media = await loadVideoControls()
  const wrap = new FakeElement('div')
  const controls = media.createFeedVideoControls()
  const video = new FakeVideo()
  wrap.appendChild(controls)

  media.bindFeedVideoControls(wrap, video)
  assert.equal(controls.getAttribute('data-feed-video-controls-visible'), '0')

  wrap.dispatch('pointerenter')
  assert.equal(controls.getAttribute('data-feed-video-controls-visible'), '1')
  wrap.dispatch('pointerleave')
  media.runPendingTimer()
  assert.equal(controls.getAttribute('data-feed-video-controls-visible'), '0')

  media.document.dispatch('keydown', { key: 'Tab' })
  wrap.dispatch('focusin')
  assert.equal(controls.getAttribute('data-feed-video-controls-visible'), '1')
  wrap.dispatch('focusout')
  media.runPendingTimer()
  assert.equal(controls.getAttribute('data-feed-video-controls-visible'), '0')
})
