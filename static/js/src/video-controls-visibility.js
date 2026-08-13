const keyboardNavigationByDocument = new WeakMap()

function keyboardNavigationState(doc) {
  let state = keyboardNavigationByDocument.get(doc)
  if (state) return state
  state = { active: false }
  doc.addEventListener('keydown', function (event) {
    if (event.key === 'Tab') state.active = true
  }, true)
  keyboardNavigationByDocument.set(doc, state)
  return state
}

export function bindVideoControlsVisibility(options) {
  const stateElement = options && options.stateElement
  const surface = options && options.surface
  if (!stateElement || !surface) return null

  const popupElements = (options.popupElements || []).filter(Boolean)
  const readyAttribute = options.readyAttribute || 'data-video-controls-ready'
  const visibleAttribute = options.visibleAttribute || 'data-video-controls-visible'
  const inactiveAttribute = options.inactiveAttribute || ''
  const hideDelay = Number(options.hideDelay == null ? 80 : options.hideDelay)
  const keyboardNavigation = keyboardNavigationState(options.document || stateElement.ownerDocument || document)
  let pointerInside = false
  let hideTimer = 0

  function popupOpen() {
    return popupElements.some(function (popup) {
      return !popup.classList.contains('hidden')
    })
  }

  function setVisible(visible) {
    const current = stateElement.getAttribute(visibleAttribute) === '1'
    if (current === visible && stateElement.hasAttribute(readyAttribute)) return
    stateElement.setAttribute(readyAttribute, '1')
    stateElement.setAttribute(visibleAttribute, visible ? '1' : '0')
    if (inactiveAttribute) {
      if (visible) stateElement.removeAttribute(inactiveAttribute)
      else stateElement.setAttribute(inactiveAttribute, '')
    }
    if (typeof options.onVisibilityChange === 'function') options.onVisibilityChange(visible)
  }

  function clearHideTimer() {
    if (!hideTimer) return
    window.clearTimeout(hideTimer)
    hideTimer = 0
  }

  function scheduleHide() {
    clearHideTimer()
    hideTimer = window.setTimeout(function () {
      hideTimer = 0
      if (!pointerInside && !popupOpen()) setVisible(false)
    }, hideDelay)
  }

  surface.addEventListener('pointerenter', function () {
    pointerInside = true
    keyboardNavigation.active = false
    clearHideTimer()
    setVisible(true)
  })
  surface.addEventListener('pointermove', function () {
    pointerInside = true
    keyboardNavigation.active = false
    clearHideTimer()
    setVisible(true)
  }, { passive: true })
  surface.addEventListener('pointerleave', function () {
    pointerInside = false
    scheduleHide()
  })
  surface.addEventListener('pointerdown', function () {
    keyboardNavigation.active = false
  }, true)
  surface.addEventListener('focusin', function () {
    if (keyboardNavigation.active) setVisible(true)
  })
  surface.addEventListener('focusout', function () {
    if (!pointerInside) scheduleHide()
  })
  popupElements.forEach(function (popup) {
    popup.addEventListener('pointerenter', function () {
      pointerInside = true
      clearHideTimer()
      setVisible(true)
    })
    popup.addEventListener('pointerleave', function () {
      pointerInside = false
      scheduleHide()
    })
  })

  setVisible(false)
  return {
    isVisible: function () {
      return stateElement.getAttribute(visibleAttribute) === '1'
    },
  }
}
