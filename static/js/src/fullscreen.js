export function bindFullscreenTransition(target, surface, { prepareEnter = () => {}, prepareExit = () => {}, finishExit = () => {} } = {}) {
  if (target._fullscreenTransition) return target._fullscreenTransition
  const properties = ['display', 'position', 'left', 'top', 'width', 'height', 'max-width', 'min-height', 'flex', 'margin', 'z-index']
  let owner
  let view
  let observer
  let pending = null
  let savedStyle = null
  let lastBounds
  let lastViewport
  let active = false
  let placeholder = null
  let windowedSpace

  function spaceFor(rect) {
    const contents = view.getComputedStyle(surface).display === 'contents'
    return { width: contents ? target.clientWidth : rect.width, height: contents ? target.clientHeight : rect.height }
  }

  function keepSpace() {
    if (!placeholder) {
      placeholder = owner.createElement('div')
      placeholder.setAttribute('aria-hidden', 'true')
      placeholder.style.visibility = 'hidden'
      surface.parentNode.insertBefore(placeholder, surface)
    }
    placeholder.style.width = windowedSpace.width + 'px'
    placeholder.style.height = windowedSpace.height + 'px'
    placeholder.style.display = 'none'
  }

  function isFullscreen() {
    return (owner.fullscreenElement || owner.webkitFullscreenElement) === target
  }

  function bounds() {
    return view.getComputedStyle(surface).display === 'contents' ? target.getBoundingClientRect() : surface.getBoundingClientRect()
  }

  function clearSurface() {
    if (placeholder) placeholder.style.display = 'none'
    surface.removeAttribute('data-fullscreen-transition')
    if (!savedStyle) return
    savedStyle.forEach(({ property, value, priority }) => {
      if (value) surface.style.setProperty(property, value, priority)
      else surface.style.removeProperty(property)
    })
  }

  function pin(rect) {
    if (!savedStyle) savedStyle = properties.map(property => ({ property, value: surface.style.getPropertyValue(property), priority: surface.style.getPropertyPriority(property) }))
    surface.setAttribute('data-fullscreen-transition', '')
    if (placeholder) placeholder.style.display = isFullscreen() ? 'none' : 'block'
    Object.entries({ display: 'block', position: 'fixed', left: '0px', top: '0px', width: rect.width + 'px', height: rect.height + 'px', 'max-width': 'none', 'min-height': '0', flex: 'none', margin: '0', 'z-index': '20001' })
      .forEach(([property, value]) => surface.style.setProperty(property, value))
    // Fixed children can still have a transformed containing block in Moments.
    const origin = surface.getBoundingClientRect()
    const scaleX = origin.width / rect.width
    const scaleY = origin.height / rect.height
    const frame = { left: (rect.x - origin.x) / scaleX + 'px', top: (rect.y - origin.y) / scaleY + 'px', width: rect.width / scaleX + 'px', height: rect.height / scaleY + 'px' }
    Object.entries(frame).forEach(([property, value]) => surface.style.setProperty(property, value))
    return { frame, scaleX, scaleY }
  }

  function rememberBounds() {
    if (pending) return
    lastBounds = bounds()
    lastViewport = [view.innerWidth, view.innerHeight]
    if (!isFullscreen()) windowedSpace = spaceFor(lastBounds)
  }

  function cancel() {
    if (pending) {
      clearTimeout(pending.timer)
      if (pending.animation) {
        pending.animation.onfinish = null
        pending.animation.cancel()
      }
      pending = null
    }
    clearSurface()
    savedStyle = null
    if (placeholder) placeholder.remove()
    placeholder = null
  }

  function begin(fullscreen, rect = bounds(), viewport = [view.innerWidth, view.innerHeight]) {
    bindOwner()
    if (fullscreen && !isFullscreen()) windowedSpace = spaceFor(rect)
    cancel()
    keepSpace()
    if (fullscreen && !active) {
      prepareEnter()
      target.setAttribute('data-fullscreen-active', '')
    }
    active = fullscreen || active
    pending = { fullscreen, rect, viewport, timer: null, animation: null }
    pin(rect)
  }

  function releaseExit() {
    active = false
    target.removeAttribute('data-fullscreen-active')
    finishExit()
  }

  function complete() {
    const transition = pending
    if (!transition || transition.animation || isFullscreen() !== transition.fullscreen) return
    if (!target.contains(surface)) {
      cancel()
      prepareExit()
      releaseExit()
      return
    }
    clearTimeout(transition.timer)
    if (!transition.fullscreen) prepareExit()
    clearSurface()
    const destination = bounds()
    if (!transition.fullscreen) {
      windowedSpace = spaceFor(destination)
      keepSpace()
    }
    const start = pin(transition.rect)
    const end = {
      left: parseFloat(start.frame.left) + (destination.x - transition.rect.x) / start.scaleX + 'px',
      top: parseFloat(start.frame.top) + (destination.y - transition.rect.y) / start.scaleY + 'px',
      width: destination.width / start.scaleX + 'px', height: destination.height / start.scaleY + 'px',
    }
    const animation = surface.animate([start.frame, end], { duration: view.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 220, easing: 'ease', fill: 'both' })
    transition.animation = animation
    animation.onfinish = function () {
      cancel()
      if (!transition.fullscreen) releaseExit()
      rememberBounds()
    }
  }

  function changed() {
    bindOwner()
    if (!isFullscreen() && active && (!pending || pending.fullscreen)) {
      begin(false, pending ? surface.getBoundingClientRect() : lastBounds, pending ? [view.innerWidth, view.innerHeight] : lastViewport)
    } else if (isFullscreen() && !active) {
      begin(true, lastBounds, lastViewport)
    }
    if (!pending || pending.animation || isFullscreen() !== pending.fullscreen) return
    pin(pending.rect)
    if (view.innerWidth !== pending.viewport[0] || view.innerHeight !== pending.viewport[1]) {
      complete()
      return
    }
    // An already fullscreen browser may not resize its viewport.
    clearTimeout(pending.timer)
    pending.timer = setTimeout(complete, 300)
  }

  function resized() {
    if (!active && !isFullscreen() && observer) return
    changed()
    rememberBounds()
  }

  function fullscreenChanged() {
    const involved = active || isFullscreen()
    changed()
    if (involved) target.dispatchEvent(new view.CustomEvent('igloo:fullscreen-change', { bubbles: true }))
  }

  function unbindOwner() {
    if (!owner) return
    owner.removeEventListener('fullscreenchange', fullscreenChanged)
    owner.removeEventListener('webkitfullscreenchange', fullscreenChanged)
    view.removeEventListener('resize', resized)
    if (observer) observer.disconnect()
  }

  function bindOwner() {
    if (owner === target.ownerDocument) return
    unbindOwner()
    owner = target.ownerDocument
    view = owner.defaultView
    owner.addEventListener('fullscreenchange', fullscreenChanged)
    owner.addEventListener('webkitfullscreenchange', fullscreenChanged)
    view.addEventListener('resize', resized)
    if (typeof view.ResizeObserver === 'function') {
      observer = new view.ResizeObserver(rememberBounds)
      observer.observe(target)
      observer.observe(surface)
    }
  }

  function exit() {
    bindOwner()
    const leave = owner.exitFullscreen || owner.webkitExitFullscreen
    if (!leave) return false
    begin(false)
    try {
      const request = leave.call(owner)
      if (request && typeof request.catch === 'function') request.catch(cancel)
      return true
    } catch (_) { cancel(); return false }
  }

  function toggle() {
    bindOwner()
    if (isFullscreen()) return exit()
    const enter = target.requestFullscreen || target.webkitRequestFullscreen
    if (!enter) return false
    begin(true)
    function failed() { cancel(); prepareExit(); releaseExit() }
    try {
      const request = enter.call(target)
      if (request && typeof request.catch === 'function') request.catch(failed)
      return true
    } catch (_) { failed(); return false }
  }

  function destroy() {
    cancel()
    unbindOwner()
    if (active) { prepareExit(); releaseExit() }
    if (target._fullscreenTransition === controller) delete target._fullscreenTransition
  }

  const controller = { cancel, toggle, exit, destroy }
  target._fullscreenTransition = controller
  bindOwner()
  rememberBounds()
  return controller
}
