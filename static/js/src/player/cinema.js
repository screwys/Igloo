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
  let manualChoice = null

  function setCinemaView(enabled) {
    const hidesPlayerSidebar = enabled && !stackedSidebar.matches && !hasChat
    root.classList.toggle('cinema-view', enabled)
    root.classList.toggle('cinema-hides-player-sidebar', hidesPlayerSidebar)
    sidebar.setAttribute('aria-hidden', hidesPlayerSidebar ? 'true' : 'false')
    button.setAttribute('aria-pressed', enabled ? 'true' : 'false')
  }

  function recommendedCinemaView() {
    return shouldAutoEnableCinema(root.getBoundingClientRect().width, stackedSidebar.matches,
      hasChat ? sidebar.getBoundingClientRect().width : PLAYER_SIDEBAR_WIDTH)
  }

  function syncCinemaView() {
    if (root.hasAttribute('data-fullscreen-active')) return
    const recommendation = recommendedCinemaView()
    setCinemaView(
      manualChoice === null ? recommendation : manualChoice,
    )
  }

  button.addEventListener('click', function () {
    const wasEnabled = root.classList.contains('cinema-view')
    const enabled = !wasEnabled
    manualChoice = enabled
    if (typeof onCinemaRequested === 'function' && onCinemaRequested(enabled)) return
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
      // Keep the page layout ready for fullscreen exit.
      sidebar.setAttribute('aria-hidden', 'false')
      return wasEnabled
    },
    restoreAfterFullscreen(enabled) {
      setCinemaView(enabled)
    },
  }
}
