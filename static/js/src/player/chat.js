import { t, materialIconMarkup, setSvgContent } from '../utils.js'

function text(value) {
  return value && (value.simpleText || value.label || (value.runs || []).map(run => run.text || '').join('')) || ''
}

function imageURL(value) {
  const thumbnails = value && value.thumbnails || []
  const url = thumbnails.length ? thumbnails[thumbnails.length - 1].url : ''
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : ''
  } catch { return '' }
}

function runs(value) {
  if (!value) return []
  if (value.simpleText) return [{ text: value.simpleText }]
  return (value.runs || []).map(run => run.emoji
    ? { image: imageURL(run.emoji.image), text: (run.emoji.shortcuts || [])[0] || run.emoji.emojiId || '' }
    : { text: run.text || '' })
}

function message(item) {
  const kind = Object.keys(item || {}).find(key => key.startsWith('liveChat') && key.endsWith('Renderer'))
  const renderer = kind && item[kind]
  if (!renderer) return null
  const header = renderer.header && renderer.header.liveChatSponsorshipsHeaderRenderer || renderer
  const badges = (header.authorBadges || []).map(badge => badge.liveChatAuthorBadgeRenderer).filter(Boolean)
  const paid = kind === 'liveChatPaidMessageRenderer' || kind === 'liveChatPaidStickerRenderer'
  const body = runs(renderer.message || renderer.headerSubtext || renderer.primaryText || renderer.text)
  if (renderer.sticker) body.push({ image: imageURL(renderer.sticker), text: text(renderer.sticker.accessibility && renderer.sticker.accessibility.accessibilityData) })
  const color = renderer.bodyBackgroundColor || renderer.backgroundColor
  return {
    id: renderer.id, authorId: header.authorExternalChannelId,
    author: text(header.authorName), avatar: imageURL(header.authorPhoto),
    badges: badges.map(badge => ({ image: imageURL(badge.customThumbnail), text: badge.tooltip || '', icon: badge.icon && badge.icon.iconType || '' })),
    body, header: runs(renderer.headerPrimaryText || renderer.header && renderer.header.liveChatSponsorshipsHeaderRenderer && renderer.header.liveChatSponsorshipsHeaderRenderer.primaryText),
    amount: text(renderer.purchaseAmountText), paid, member: kind.includes('Membership') || kind.includes('Sponsorship'),
    owner: badges.some(badge => badge.icon && badge.icon.iconType === 'OWNER'),
    moderator: badges.some(badge => badge.icon && badge.icon.iconType === 'MODERATOR'),
    color: paid && Number.isInteger(color) ? '#' + (color >>> 0).toString(16).padStart(8, '0').slice(2) : '',
    timestamp: Number(renderer.timestampUsec) / 1000 || 0,
  }
}

// yt-dlp emits both live messages and replays as chat actions in JSON lines.
export function chatEvents(record, captureStartedAt, liveStream = !!record.isLive) {
  const replay = record.replayChatItemAction
  const live = liveStream
  const offset = Number(replay && replay.videoOffsetTimeMsec || record.videoOffsetTimeMsec || 0)
  const actions = replay ? replay.actions || [] : [record]
  const out = []
  for (const action of actions) {
    const add = action.addChatItemAction
    const replace = action.replaceChatItemAction
    const banner = action.addBannerToLiveChatCommand
    const item = add && add.item || replace && replace.replacementItem || banner && banner.bannerRenderer && banner.bannerRenderer.liveChatBannerRenderer && banner.bannerRenderer.liveChatBannerRenderer.contents
    const entry = message(item)
    const time = live ? entry && entry.timestamp || captureStartedAt + offset : offset
    if (entry) {
      if (replace) entry.id = replace.targetItemId
      if (banner) { entry.id = banner.bannerRenderer.liveChatBannerRenderer.bannerId; entry.pinned = true }
      out.push({ time, live, type: replace ? 'replace' : 'add', message: entry })
    }
    const remove = action.removeChatItemAction || action.markChatItemAsDeletedAction
    const removeAuthor = action.removeChatItemByAuthorAction || action.markChatItemsByAuthorAsDeletedAction
    const removeBanner = action.removeBannerForLiveChatCommand
    if (remove || removeBanner) out.push({ time, live, type: 'remove', id: remove ? remove.targetItemId : removeBanner.bannerId })
    if (removeAuthor) out.push({ time, live, type: 'removeAuthor', authorId: removeAuthor.externalChannelId })
  }
  return out
}

function appendRuns(element, values) {
  for (const value of values || []) {
    if (value.image) {
      const image = document.createElement('img')
      image.src = value.image
      image.alt = value.text
      image.title = value.text
      image.className = 'chat-emote'
      element.appendChild(image)
    } else element.appendChild(document.createTextNode(value.text))
  }
}

function messageRow(entry) {
  const row = document.createElement('div')
  row.className = 'chat-message'
  row.classList.toggle('chat-paid', entry.paid)
  row.classList.toggle('chat-member', entry.member)
  row.classList.toggle('chat-pinned', !!entry.pinned)
  row.classList.toggle('chat-owner', entry.owner)
  row.classList.toggle('chat-moderator', entry.moderator)
  if (entry.color) row.style.setProperty('--chat-paid-color', entry.color)
  if (entry.avatar) {
    const avatar = document.createElement('img')
    avatar.className = 'chat-avatar'
    avatar.src = entry.avatar
    avatar.alt = ''
    avatar.loading = 'lazy'
    row.appendChild(avatar)
  }
  const content = document.createElement('div')
  content.className = 'chat-message-content'
  for (const badge of entry.badges) {
    const label = document.createElement('span')
    label.className = 'chat-badge'
    label.title = badge.text
    if (badge.image) appendRuns(label, [{ image: badge.image, text: badge.text }])
    else {
      setSvgContent(label, materialIconMarkup(badge.icon === 'MODERATOR' ? 'Settings' : 'Check'))
      label.setAttribute('aria-label', badge.text)
    }
    content.appendChild(label)
  }
  const author = document.createElement('strong')
  author.className = 'chat-author'
  author.textContent = entry.author
  content.append(author, document.createTextNode(' '))
  if (entry.amount) {
    const amount = document.createElement('strong')
    amount.className = 'chat-amount'
    amount.textContent = entry.amount
    content.appendChild(amount)
  }
  if (entry.header.length) {
    const header = document.createElement('div')
    appendRuns(header, entry.header)
    content.appendChild(header)
  }
  appendRuns(content, entry.body)
  row.appendChild(content)
  return row
}

export function initLiveChat(video, root) {
  const panel = document.getElementById('player-chat')
  if (!panel) return
  const list = document.getElementById('player-chat-messages')
  const status = document.getElementById('player-chat-status')
  const latest = document.getElementById('player-chat-latest')
  const toggle = document.getElementById('player-chat-toggle')
  const refresh = document.getElementById('player-chat-refresh')
  const body = document.getElementById('player-chat-body')
  let events = []
  const messages = new Map()
  const messageOrder = []
  let cursor = 0
  let previousTime = -Infinity
  let captureStartedAt = 0
  const live = panel.dataset.live === '1'
  let liveEpochOffset = null
  let streamPosition = null
  let follow = true
  let visibleCount = 100
  let source = null
  let animation = null
  let ended = false
  let renderPending = false
  let viewEnd = null
  let hasEarlier = false

  function position() {
    const mediaTime = video.currentTime * 1000
    if (!live) return mediaTime
    const time = streamPosition && streamPosition()
    if (Number.isFinite(time)) liveEpochOffset = time - mediaTime
    return liveEpochOffset === null ? -Infinity : liveEpochOffset + mediaTime
  }

  function render(prepend = false) {
    const entries = []
    let index = follow || viewEnd === null ? messageOrder.length - 1 : viewEnd
    for (; index >= 0 && entries.length < visibleCount; index--) {
      const entry = messageOrder[index]
      if (messages.get(entry.id) === entry) entries.push(entry)
    }
    hasEarlier = index >= 0
    entries.reverse()
    const height = list.scrollHeight
    const scroll = list.scrollTop
    list.replaceChildren(...entries.map(messageRow))
    if (follow) list.scrollTop = list.scrollHeight
    else list.scrollTop = scroll + (prepend ? Math.max(0, list.scrollHeight - height) : 0)
    latest.classList.toggle('hidden', follow)
  }

  function apply(event) {
    if (event.type === 'add' || event.type === 'replace') {
      if (!event.message.id) return
      const existing = messages.get(event.message.id)
      const orderIndex = existing ? existing.orderIndex : messageOrder.length
      const entry = { ...event.message, orderIndex }
      messageOrder[orderIndex] = entry
      messages.set(entry.id, entry)
    } else if (event.type === 'remove') messages.delete(event.id)
    else if (event.type === 'removeAuthor') {
      for (const [id, entry] of messages) if (entry.authorId === event.authorId) messages.delete(id)
    }
  }

  function advance() {
    animation = null
    if (body.hidden) return
    const time = position()
    let changed = renderPending
    renderPending = false
    if (time < previousTime || video.seeking) {
      messages.clear()
      messageOrder.length = 0
      viewEnd = null
      cursor = 0
      visibleCount = 100
      changed = true
    }
    while (cursor < events.length && events[cursor].time <= time) {
      apply(events[cursor++])
      changed = true
    }
    previousTime = time
    if (changed) render()
  }

  function schedule() {
    if (animation === null) animation = requestAnimationFrame(advance)
  }

  function close() {
    if (source) source.close()
    source = null
  }

  function connect() {
    if (source || ended) return
    refresh.classList.add('hidden')
    if (!live) {
      events = []
      messages.clear()
      messageOrder.length = 0
      cursor = 0
      previousTime = -Infinity
    }
    source = new EventSource('/api/youtube/' + encodeURIComponent(root.dataset.videoId) + '/chat')
    source.addEventListener('start', event => { captureStartedAt = JSON.parse(event.data).started_at_ms })
    source.addEventListener('chat', event => {
      const incoming = chatEvents(JSON.parse(event.data), captureStartedAt, live)
      if (!incoming.length) return
      if (events.length && incoming[0].time < events[events.length - 1].time) {
        events.push(...incoming)
        events.sort((a, b) => a.time - b.time)
        previousTime = -Infinity
        messages.clear()
        messageOrder.length = 0
        cursor = 0
      } else events.push(...incoming)
      status.textContent = ''
      if (!video.paused || incoming[0].time <= position() || previousTime === -Infinity) schedule()
    })
    function finish(label, failed = false) {
      close()
      ended = true
      status.textContent = label
      refresh.classList.toggle('hidden', !failed)
      schedule()
    }
    source.addEventListener('end', () => finish(live ? t('player_chat_ended', 'Chat ended') : ''))
    source.addEventListener('unavailable', () => finish(t('player_chat_unavailable', 'Chat unavailable'), true))
    source.addEventListener('failed', () => finish(t('player_chat_failed', 'Chat could not load'), true))
    source.onerror = () => finish(t('player_chat_failed', 'Chat could not load'), true)
  }

  list.addEventListener('scroll', () => {
    const atBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 24
    if (follow && !atBottom) viewEnd = messageOrder.length - 1
    follow = atBottom
    latest.classList.toggle('hidden', follow)
    if (!follow && list.scrollTop < 24 && hasEarlier) {
      visibleCount += 100
      render(true)
    }
  }, { passive: true })
  latest.addEventListener('click', () => { follow = true; visibleCount = 100; render() })
  refresh.addEventListener('click', () => {
    ended = false
    events = []
    messages.clear()
    messageOrder.length = 0
    viewEnd = null
    cursor = 0
    previousTime = -Infinity
    renderPending = true
    status.textContent = t('status_loading_ellipsis', 'Loading...')
    connect()
  })
  toggle.addEventListener('click', () => {
    body.hidden = !body.hidden
    root.classList.toggle('chat-closed', body.hidden)
    toggle.setAttribute('aria-expanded', String(!body.hidden))
    toggle.title = body.hidden ? t('action_show', 'Show') : t('action_hide', 'Hide')
    toggle.setAttribute('aria-label', toggle.title)
    setSvgContent(toggle, materialIconMarkup(body.hidden ? 'KeyboardArrowDown' : 'KeyboardArrowUp'))
    if (body.hidden) close()
    else { connect(); schedule() }
  })
  video.addEventListener('timeupdate', schedule)
  video.addEventListener('seeked', () => { follow = true; schedule() })
  video.addEventListener('seeking', schedule)
  video.addEventListener('playing', () => {
    if (liveEpochOffset === null) liveEpochOffset = Date.now() - video.currentTime * 1000
    schedule()
  })
  root.addEventListener('streamclockready', event => { streamPosition = event.detail.position; schedule() })
  if (!video.paused && liveEpochOffset === null) liveEpochOffset = Date.now() - video.currentTime * 1000
  window.addEventListener('pagehide', () => { close(); if (animation !== null) cancelAnimationFrame(animation); animation = null })
  window.addEventListener('pageshow', () => { if (!body.hidden) connect() })
  connect()
}
