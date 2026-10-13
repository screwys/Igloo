import { apiFetch, t, tf } from '../utils.js'
import { createXLivePlayback } from '../player/x-live.js'
import { bindFeedVideoControls, createFeedVideoControls } from './video-controls.js'
import { closeMediaOverlay, getOverlayElement, openMediaOverlay } from './media-overlay.js'

function playbackOwner() {
  const host = window.top
  if (!host.IglooSpacePlayer) host.IglooSpacePlayer = { current: null }
  return host.IglooSpacePlayer
}

function miniPlayer() {
  return window.top.IglooMiniPlayer
}

export function syncSpaceSidebar() {
  const session = playbackOwner().current
  const sidebar = document.getElementById('feed-lives')
  if (!session || !sidebar || session.sourceDocument !== document) return
  sidebar.hidden = false
  sidebar.querySelectorAll('.feed-live-row').forEach(function (row) {
    if (row !== session.row && row.dataset.feedLiveUrl === session.url) row.remove()
  })
}

function avatar(parent, url, name) {
  const wrap = document.createElement('span')
  wrap.className = 'feed-avatar'
  const fallback = document.createElement('span')
  fallback.className = 'feed-avatar-fallback'
  fallback.textContent = (name.charAt(0) || 'X').toUpperCase()
  wrap.appendChild(fallback)
  if (url) {
    const image = document.createElement('img')
    image.className = 'feed-avatar-image'
    image.alt = ''
    image.addEventListener('load', function () {
      image.style.display = ''
      fallback.style.display = 'none'
    })
    image.addEventListener('error', function () {
      if (window.MpaSiteBase && window.MpaSiteBase.avatarError(image)) return
      image.style.display = 'none'
      fallback.style.display = ''
    })
    image.src = url
    wrap.appendChild(image)
  }
  parent.appendChild(wrap)
}

export function playSpace(root) {
  const url = root.dataset.feedLiveUrl
  const owner = playbackOwner()
  if (owner.current && owner.current.url === url) {
    owner.current.expand()
    return
  }
  if (owner.current) owner.current.stop()
  const slot = document.getElementById('feed-live-active')
  if (!slot) {
    openMediaOverlay(root, root)
    return
  }
  let row = root.closest('.feed-live-row')
  if (!row) {
    row = root
    row.className = 'feed-live-row'
    const header = document.createElement('div')
    header.className = 'story-channel-row'
    const avatars = document.createElement('span')
    avatars.className = 'feed-space-avatars'
    avatar(avatars, root.dataset.avatarUrl || '', root.dataset.channelName || 'X')
    const copy = document.createElement('button')
    copy.className = 'story-channel-copy'
    copy.type = 'button'
    const name = document.createElement('span')
    name.className = 'story-channel-name feed-author'
    name.textContent = root.dataset.channelName || 'X'
    copy.appendChild(name)
    header.append(avatars, copy)
    row.appendChild(header)
  }
  const header = row.querySelector('.story-channel-row')
  const avatars = header.querySelector('.feed-space-avatars')
  const trigger = header.querySelector('.story-channel-copy')
  slot.appendChild(row)
  row.classList.add('feed-space-active')
  const stage = document.createElement('div')
  stage.className = 'feed-space-player'
  const content = document.createElement('div')
  content.setAttribute('data-fullscreen-content', '')
  stage.appendChild(content)
  const title = document.createElement('div')
  title.className = 'feed-space-title'
  title.textContent = root.dataset.feedLiveTitle || ''
  const heading = document.createElement('div')
  heading.className = 'feed-space-heading'
  heading.appendChild(title)
  const leaveButton = document.createElement('button')
  leaveButton.type = 'button'
  leaveButton.className = 'btn btn-secondary feed-space-leave'
  leaveButton.textContent = t('space_leave', 'Leave')
  heading.appendChild(leaveButton)
  content.appendChild(heading)
  const video = document.createElement('video')
  video.playsInline = true
  video.className = 'feed-space-video'
  content.appendChild(video)
  const speakers = document.createElement('div')
  speakers.className = 'feed-space-speakers'
  content.appendChild(speakers)
  const rosterStatus = document.createElement('div')
  rosterStatus.className = 'feed-space-status'
  rosterStatus.setAttribute('role', 'status')
  rosterStatus.hidden = true
  content.appendChild(rosterStatus)
  const playbackStatus = document.createElement('div')
  playbackStatus.className = 'feed-space-status'
  playbackStatus.setAttribute('role', 'status')
  content.appendChild(playbackStatus)
  const controls = createFeedVideoControls({ refresh: true, mini: false, cinema: false })
  content.appendChild(controls)
  row.appendChild(stage)
  let playback = null
  let rosterTimer = null
  let rosterAbort = null
  let participantElements = []
  const overflow = document.createElement('span')
  overflow.className = 'feed-space-overflow feed-avatar'
  overflow.setAttribute('role', 'img')
  overflow.hidden = true
  const overflowCount = document.createElement('span')
  overflowCount.className = 'feed-avatar-fallback'
  overflow.appendChild(overflowCount)

  function layoutParticipants() {
    if (!participantElements.length) return
    if (stage.closest('.feed-space-overlay')) {
      participantElements.forEach(function (person) { person.hidden = false })
      overflow.hidden = true
      return
    }
    const compact = participantElements.filter(function (person) {
      return person.dataset.role !== 'host' && person.dataset.identity !== root.dataset.channelId
    })
    participantElements.forEach(function (person) { person.hidden = !compact.includes(person) })
    if (!compact.length) { overflow.hidden = true; return }
    const headerStyle = getComputedStyle(header)
    const avatarStyle = getComputedStyle(avatars)
    const width = header.clientWidth - parseFloat(headerStyle.paddingLeft) - parseFloat(headerStyle.paddingRight)
      - parseFloat(headerStyle.columnGap) - parseFloat(getComputedStyle(trigger).minWidth)
      - avatars.firstElementChild.getBoundingClientRect().width
      - parseFloat(avatarStyle.paddingLeft) - parseFloat(avatarStyle.paddingRight)
    const style = getComputedStyle(compact[0])
    const size = parseFloat(style.width)
    const gap = parseFloat(style.marginLeft)
    const capacity = Math.floor(Math.max(0, width) / (size + gap))
    const shown = compact.length <= capacity ? compact.length : Math.max(0, capacity - 1)
    compact.forEach(function (person, index) { person.hidden = index >= shown })
    const remaining = compact.length - shown
    overflowCount.textContent = '+' + remaining
    overflow.title = tf('space_more_speakers', '%1$d more speakers', remaining)
    overflow.setAttribute('aria-label', overflow.title)
    overflow.hidden = !remaining
  }
  const resizeObserver = new ResizeObserver(layoutParticipants)
  resizeObserver.observe(header)

  const session = {
    row, url, video, element: stage, sourceDocument: document,
    restore: function () {
      row.appendChild(stage)
      avatars.appendChild(speakers)
      trigger.setAttribute('aria-expanded', 'false')
      layoutParticipants()
    },
    stop: function () {
      if (owner.current !== session) return
      owner.current = null
      if (rosterTimer) clearTimeout(rosterTimer)
      if (rosterAbort) rosterAbort.abort()
      resizeObserver.disconnect()
      row.removeEventListener('click', openFromRow)
      const overlay = getOverlayElement()
      if (overlay && overlay._spaceSurface === session) closeMediaOverlay()
      if (playback) playback.stop()
      if (unbindControls) unbindControls()
      stage.remove()
      speakers.remove()
      row.classList.remove('feed-space-active')
      trigger.setAttribute('aria-expanded', 'false')
      delete video._spaceSurface
      const manager = miniPlayer()
      if (manager && manager.closeSurface) manager.closeSurface(row)
      const list = document.querySelector('#feed-lives .stories-list')
      if (list) list.appendChild(row)
    },
  }
  owner.current = session
  session.miniSurface = {
    element: row, video, kind: 'space', sourceDocument: document,
    title: title.textContent || root.dataset.channelName || t('platform_x', 'X'),
    onPause: session.stop,
    onDock: function () {
      const overlay = getOverlayElement()
      if (overlay && overlay._spaceSurface === session) closeMediaOverlay()
    },
  }
  video._spaceSurface = session.miniSurface
  function expand() {
    openMediaOverlay(root, root, session)
    content.insertBefore(speakers, rosterStatus)
    trigger.setAttribute('aria-expanded', 'true')
    layoutParticipants()
  }
  session.expand = expand
  function openFromRow(event) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    if (event.defaultPrevented || event.target.closest('[data-feed-video-control], [data-feed-progress]')) return
    if (owner.current !== session) return
    event.preventDefault()
    event.stopPropagation()
    expand()
  }
  row.addEventListener('click', openFromRow)
  const unbindControls = bindFeedVideoControls(stage, video, {
    onRefresh: function () {
      playback.refresh()
      loadParticipants()
    },
  })
  leaveButton.addEventListener('click', function (event) {
    event.preventDefault()
    event.stopPropagation()
    session.stop()
  })
  video.addEventListener('loadedmetadata', function () {
    stage.classList.toggle('has-video', video.videoWidth > 0)
  })
  video.addEventListener('click', function () {
    if (video.paused) video.play().catch(function () {})
    else video.pause()
  })

  function renderParticipants(participants) {
    const previous = new Map(participantElements.map(function (person) { return [person.dataset.identity, person] }))
    participantElements = participants.map(function (participant, index) {
      const name = participant.display_name || participant.handle || t('platform_x', 'X')
      const identity = participant.channel_id || participant.handle || String(index)
      const person = previous.get(identity) || document.createElement(participant.channel_id ? 'a' : 'span')
      previous.delete(identity)
      person.dataset.identity = identity
      person.dataset.role = participant.role
      person.className = 'feed-space-speaker'
      if (participant.channel_id) {
        person.classList.add('feed-inline-link')
        person.href = '/channels/' + encodeURIComponent(participant.channel_id)
      }
      person.title = participant.handle ? '@' + participant.handle : name
      person.setAttribute('aria-label', name)
      const image = person.querySelector('.feed-avatar-image')
      if (!person.querySelector('.feed-avatar') || (participant.avatar_url && (!image || !image.naturalWidth || image.getAttribute('src') !== participant.avatar_url))) {
        const oldAvatar = person.querySelector('.feed-avatar')
        if (oldAvatar) oldAvatar.remove()
        avatar(person, participant.avatar_url, name)
      }
      const label = person.querySelector('.feed-space-speaker-name') || document.createElement('span')
      label.className = 'feed-space-speaker-name'
      label.textContent = name
      person.appendChild(label)
      const role = person.querySelector('.feed-space-speaker-role') || document.createElement('span')
      role.className = 'feed-space-speaker-role'
      role.textContent = participant.role === 'host' ? t('space_host', 'Host')
        : participant.role === 'co_host' ? t('space_co_host', 'Co-host') : t('space_speaker', 'Speaker')
      person.appendChild(role)
      return person
    })
    previous.forEach(function (person) { person.remove() })
    participantElements.forEach(function (person, index) {
      if (speakers.children[index] !== person) speakers.insertBefore(person, speakers.children[index] || null)
    })
    speakers.appendChild(overflow)
    layoutParticipants()
  }

  async function loadParticipants() {
    if (rosterTimer) clearTimeout(rosterTimer)
    if (rosterAbort) rosterAbort.abort()
    const request = new AbortController()
    rosterAbort = request
    const spaceID = new URL(url).pathname.split('/').pop()
    try {
      const result = await apiFetch('/api/x/spaces/' + encodeURIComponent(spaceID) + '/participants', { signal: request.signal })
      if (owner.current !== session || rosterAbort !== request) return
      renderParticipants(result.participants)
      rosterStatus.hidden = true
      rosterTimer = setTimeout(loadParticipants, 30000)
    } catch (error) {
      if (request.signal.aborted || owner.current !== session) return
      rosterStatus.textContent = error.payload && error.payload.error_code === 'x_auth_required'
        ? t('space_cookies_required', 'X cookies required for speakers') : t('space_speakers_unavailable', 'Speakers unavailable')
      rosterStatus.hidden = false
    }
  }

  document.querySelectorAll('video[data-feed-inline-video]').forEach(function (other) { other.pause() })
  playback = createXLivePlayback(video, url, {
    onStatus: function (message) {
      playbackStatus.textContent = message
      playbackStatus.hidden = !message
    },
    onLive: function (live) {
      title.textContent = live.title || ''
      session.miniSurface.title = live.title || live.display_name || live.handle || t('platform_x', 'X')
      root.dataset.channelId = live.channel_id || ''
      root.dataset.channelName = live.display_name || live.handle || ''
      root.dataset.feedLiveTitle = live.title || ''
      const author = root.querySelector('.feed-author')
      if (author) author.textContent = live.display_name || live.handle || t('platform_x', 'X')
      if (!participantElements.length) {
        renderParticipants([{ channel_id: live.channel_id, display_name: live.display_name,
          handle: live.handle, avatar_url: live.avatar_url, role: 'host' }])
      }
    },
  })
  loadParticipants()
  syncSpaceSidebar()
  expand()
  return session
}

document.body.addEventListener('htmx:afterSwap', syncSpaceSidebar)
window.addEventListener('pagehide', function () {
  const session = playbackOwner().current
  if (session && session.sourceDocument === document) session.stop()
})
