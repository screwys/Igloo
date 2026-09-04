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
    this.blurred = false
    this.classList = new FakeClassList(this)
    this.style = {
      values: new Map(),
      setProperty: (name, value) => this.style.values.set(name, value),
      removeProperty: (name) => this.style.values.delete(name),
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

  removeEventListener(name, listener) {
    const listeners = this.listeners.get(name) || []
    this.listeners.set(name, listeners.filter((candidate) => candidate !== listener))
  }

  blur() {
    this.blurred = true
  }

  dispatch(name, details = {}) {
    const event = { preventDefault() {}, stopPropagation() {}, stopImmediatePropagation() {}, ...details }
    for (const listener of this.listeners.get(name) || []) listener(event)
  }

  querySelector(selector) {
    const match = /^\[([^=\]]+)(?:="([^"]*)")?\]$/.exec(selector)
    if (match && this.attributes.has(match[1]) && (match[2] === undefined || this.attributes.get(match[1]) === match[2])) return this
    if (String(selector).toLowerCase() === String(this.tagName).toLowerCase()) return this
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
    if (String(selector).toLowerCase() === String(this.tagName).toLowerCase()) matches.push(this)
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
    this.currentTime = 0
    this.duration = 60
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
  const volumeSource = await readFile(new URL('./src/volume.js', import.meta.url), 'utf8')
  const runnable = "const attachSeekTooltip = () => {}; const makeDraggableSeekbar = () => {}; const materialIconMarkup = (name) => '<svg>' + name + '</svg>'; const setSvgContent = (element, html) => { element.innerHTML = html }; const t = (_key, fallback) => fallback; const tf = (_key, fallback) => fallback;\n" +
    volumeSource.replace(/\bexport\s+/g, '') + '\n' +
    visibilitySource.replace(/\bexport\s+/g, '') + '\n' +
    source.replace(/^import .*$/gm, '').replace(/\bexport\s+/g, '') +
    '\nObject.assign(globalThis, { createFeedVideoControls, bindFeedVideoControls, exitFeedVideoFullscreen, handleFeedVideoShortcut, toggleFeedVideoFullscreen, toggleFeedVideoMute });'

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
  const storedValues = new Map()
  window.localStorage = {
    getItem(key) { return storedValues.has(key) ? storedValues.get(key) : null },
    setItem(key, value) { storedValues.set(key, value) },
  }
  window.top = window
  const document = new FakeElement('document')
  document.createElement = (tagName) => new FakeElement(tagName)
  const context = vm.createContext({ document, window, HTMLVideoElement: FakeVideo })
  vm.runInContext(runnable, context, { filename: 'video-controls.js' })
  context.runPendingTimer = function () {
    const callback = pendingTimer
    pendingTimer = null
    if (callback) callback()
  }
  context.storedValues = storedValues
  return context
}

test('integrated volume control changes and persists feed volume without toggling playback', async () => {
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
  assert.equal(popover.style.values.get('--feed-volume-height'), '22px')
  assert.equal(media.storedValues.get('feedVolume'), '0.35')

  const nextWrap = new FakeElement('div')
  const nextControls = media.createFeedVideoControls()
  const nextVideo = new FakeVideo()
  nextWrap.appendChild(nextControls)
  media.bindFeedVideoControls(nextWrap, nextVideo)
  assert.equal(nextVideo.volume, 0.35)

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

test('feed volume popover closes after pointer adjustment and stays closed when controls return', async () => {
  const media = await loadVideoControls()
  const wrap = new FakeElement('div')
  const controls = media.createFeedVideoControls()
  const video = new FakeVideo()
  wrap.appendChild(controls)

  media.bindFeedVideoControls(wrap, video)
  const slider = controls.querySelector('[data-feed-video-volume]')
  const volumeControl = controls.querySelector('[data-feed-video-volume-control]')

  wrap.dispatch('pointerenter')
  volumeControl.dispatch('pointerenter')
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), true)

  slider.dispatch('pointerup')
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), false)
  assert.equal(slider.blurred, true)

  volumeControl.dispatch('pointerenter')
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), true)
  wrap.dispatch('pointerleave')
  media.runPendingTimer()
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), false)
  wrap.dispatch('pointerenter')
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), false)

  volumeControl.dispatch('focusin')
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), true)
  volumeControl.dispatch('focusout', { relatedTarget: null })
  assert.equal(volumeControl.hasAttribute('data-feed-video-volume-open'), false)
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

test('feed controls always offer manual mini-player docking', async () => {
  const media = await loadVideoControls()
  const wrap = new FakeElement('div')
  const controls = media.createFeedVideoControls()
  const video = new FakeVideo()
  wrap.appendChild(video)
  wrap.appendChild(controls)

  let surface = null
  media.window.IglooMiniPlayer = {
    toggleSurface(next) { surface = next },
  }
  media.bindFeedVideoControls(wrap, video)

  const mini = controls.querySelector('[data-feed-video-mini]')
  assert.ok(mini)
  mini.dispatch('click')
  assert.equal(surface.element, wrap)
  assert.equal(surface.video, video)
  assert.equal(surface.kind, 'feed')
  assert.equal(surface.title, undefined)
  assert.equal(video.paused, true)
})

test('feed cinema and fullscreen controls own different presentation modes', async () => {
  const media = await loadVideoControls()
  const wrap = new FakeElement('div')
  const controls = media.createFeedVideoControls()
  const video = new FakeVideo()
  wrap.appendChild(video)
  wrap.appendChild(controls)

  let opened = null
  media.window.FeedMediaOverlay = {
    open(root, trigger) { opened = { root, trigger } },
  }
  video.requestFullscreen = function () {
    media.document.fullscreenElement = video
    media.document.dispatch('fullscreenchange')
    return Promise.resolve()
  }
  media.document.exitFullscreen = function () {
    media.document.fullscreenElement = null
    media.document.dispatch('fullscreenchange')
    return Promise.resolve()
  }

  media.bindFeedVideoControls(wrap, video)
  const cinema = controls.querySelector('[data-feed-video-cinema]')
  const fullscreen = controls.querySelector('[data-feed-video-fullscreen]')
  assert.ok(cinema)
  assert.ok(fullscreen)
  assert.equal(cinema.getAttribute('aria-pressed'), null)

  cinema.dispatch('click')
  assert.deepEqual(opened, { root: wrap, trigger: wrap })
  assert.equal(media.document.fullscreenElement, undefined)

  fullscreen.dispatch('click')
  assert.equal(media.document.fullscreenElement, video)
  assert.equal(fullscreen.getAttribute('aria-label'), 'Exit fullscreen')
  assert.equal(fullscreen.classList.contains('active'), false)

  video.dispatch('dblclick')
  assert.equal(media.document.fullscreenElement, null)
  assert.equal(fullscreen.getAttribute('aria-label'), 'Enter fullscreen')
})

test('video shortcuts seek, change volume, play, and mute', async () => {
  const media = await loadVideoControls()
  const video = new FakeVideo()
  media.window.cfShortcuts = {
    match(id, key) { return id === 'feed.mute' && key.toLowerCase() === 'm' },
  }

  video.currentTime = 10
  assert.equal(media.handleFeedVideoShortcut({ key: 'ArrowRight' }, video), true)
  assert.equal(video.currentTime, 15)
  assert.equal(media.handleFeedVideoShortcut({ key: 'ArrowLeft' }, video), true)
  assert.equal(video.currentTime, 10)

  video.volume = 0.5
  assert.equal(media.handleFeedVideoShortcut({ key: 'ArrowUp' }, video), true)
  assert.equal(video.volume, 0.55)
  assert.equal(video.muted, false)
  assert.equal(media.handleFeedVideoShortcut({ key: 'ArrowDown' }, video), true)
  assert.equal(video.volume, 0.5)

  assert.equal(media.handleFeedVideoShortcut({ key: ' ' }, video), true)
  assert.equal(video.paused, false)
  assert.equal(media.handleFeedVideoShortcut({ key: 'M' }, video), true)
  assert.equal(video.muted, true)
})

test('GIF overlays retain arrow navigation while still accepting mute', async () => {
  const media = await loadVideoControls()
  const video = new FakeVideo()
  media.window.cfShortcuts = {
    match(id, key) { return id === 'feed.mute' && key.toLowerCase() === 'm' },
  }
  video.currentTime = 10

  assert.equal(media.handleFeedVideoShortcut({ key: 'ArrowRight' }, video, { seek: false }), false)
  assert.equal(video.currentTime, 10)
  assert.equal(media.handleFeedVideoShortcut({ key: 'm' }, video, { seek: false }), true)
  assert.equal(video.muted, false)
})

test('moments toolbox options support omitting mini/cinema and binding autoplay', async () => {
  const media = await loadVideoControls()
  const wrap = new FakeElement('div')
  const controls = media.createFeedVideoControls({ mini: false, cinema: false, autoplay: true })
  const video = new FakeVideo()
  wrap.appendChild(video)
  wrap.appendChild(controls)

  assert.equal(controls.querySelector('[data-feed-video-mini]'), null)
  assert.equal(controls.querySelector('[data-feed-video-cinema]'), null)
  const autoplay = controls.querySelector('[data-feed-video-autoplay]')
  assert.ok(autoplay)
  assert.ok(controls.querySelector('[data-feed-video-play]'))
  assert.ok(controls.querySelector('[data-feed-progress]'))
  assert.ok(controls.querySelector('[data-feed-video-speed]'))
  assert.ok(controls.querySelector('[data-feed-video-volume-control]'))
  assert.ok(controls.querySelector('[data-feed-video-fullscreen]'))

  let autoplayState = false
  let toggled = false
  media.bindFeedVideoControls(wrap, video, {
    mini: false,
    cinema: false,
    autoplay: true,
    getAutoplay() { return autoplayState },
    onAutoplayToggle() {
      toggled = true
      autoplayState = !autoplayState
    },
  })

  assert.equal(autoplay.getAttribute('aria-pressed'), 'false')
  autoplay.dispatch('click')
  assert.equal(toggled, true)
  assert.equal(autoplayState, true)
  assert.equal(autoplay.getAttribute('aria-pressed'), 'true')
})
