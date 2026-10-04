export const CINEMA_MIN_PLAYER_WIDTH = 720
export const PLAYER_SIDEBAR_WIDTH = 320
export const PLAYER_MAIN_HORIZONTAL_PADDING = 40

export function shouldAutoEnableCinema(layoutWidth, sidebarIsStacked, sidebarWidth = PLAYER_SIDEBAR_WIDTH) {
  if (sidebarIsStacked) return false
  return layoutWidth - sidebarWidth - PLAYER_MAIN_HORIZONTAL_PADDING < CINEMA_MIN_PLAYER_WIDTH
}

export function initCinemaView({ root, button, onCinemaRequested }) {
  const sidebar = root && root.querySelector('.player-sidebar')
  if (!root || !button || !sidebar) return

  const stackedSidebar = window.matchMedia('(max-width: 1024px)')
  const hasChat = root.classList.contains('has-live-chat')
  const navigationSidebar = root.ownerDocument?.querySelector('#app-sidebar')
  let navigationWidthBeforeCinema = null
  let manualChoice = null
  let suspendedForFullscreen = false

  function setCinemaView(enabled, notifySidebar) {
    const changed = root.classList.contains('cinema-view') !== enabled
    if (enabled && navigationWidthBeforeCinema === null && navigationSidebar) {
      navigationWidthBeforeCinema = navigationSidebar.getBoundingClientRect().width
    }
    const hidesPlayerSidebar = enabled && !stackedSidebar.matches && !hasChat
    root.classList.toggle('cinema-view', enabled)
    root.classList.toggle('cinema-hides-player-sidebar', hidesPlayerSidebar)
    if (changed && notifySidebar !== false && typeof CustomEvent === 'function' && typeof root.dispatchEvent === 'function') {
      root.dispatchEvent(new CustomEvent('igloo:cinema-sidebar-change', {
        bubbles: true,
        detail: {
          enabled,
        },
      }))
    }
    sidebar.setAttribute('aria-hidden', hidesPlayerSidebar ? 'true' : 'false')
    button.setAttribute('aria-pressed', enabled ? 'true' : 'false')
    if (!enabled && notifySidebar !== false) navigationWidthBeforeCinema = null
  }

  function recommendedCinemaView() {
    // Judge automatic cinema against the space available before compacting navigation.
    const extraWidth = navigationWidthBeforeCinema !== null && navigationSidebar
      ? navigationWidthBeforeCinema - navigationSidebar.getBoundingClientRect().width : 0
    return shouldAutoEnableCinema(root.getBoundingClientRect().width - extraWidth, stackedSidebar.matches,
      hasChat ? sidebar.getBoundingClientRect().width : PLAYER_SIDEBAR_WIDTH)
  }

  function syncCinemaView() {
    if (suspendedForFullscreen) return
    const recommendation = recommendedCinemaView()
    setCinemaView(
      manualChoice === null ? recommendation : manualChoice,
    )
  }

  button.addEventListener('click', function () {
    const wasEnabled = root.classList.contains('cinema-view')
    const enabled = !wasEnabled
    if (typeof onCinemaRequested === 'function' && onCinemaRequested(enabled)) return
    manualChoice = enabled
    setCinemaView(enabled)
  })

  if (typeof window.ResizeObserver === 'function') {
    const observer = new window.ResizeObserver(syncCinemaView)
    observer.observe(root)
    observer.observe(sidebar)
  } else {
    window.addEventListener('resize', syncCinemaView)
  }
  stackedSidebar.addEventListener('change', syncCinemaView)
  syncCinemaView()

  return {
    suspendForFullscreen() {
      const wasEnabled = root.classList.contains('cinema-view')
      suspendedForFullscreen = true
      setCinemaView(false, false)
      return wasEnabled
    },
    restoreAfterFullscreen(enabled) {
      suspendedForFullscreen = false
      setCinemaView(enabled)
    },
  }
}
