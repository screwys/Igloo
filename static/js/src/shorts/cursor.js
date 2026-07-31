export function readMomentsCursor(request, options) {
  var opts = options || {}
  var scheduleTimeout = opts.scheduleTimeout || setTimeout
  var cancelTimeout = opts.cancelTimeout || clearTimeout
  var timeoutMs = Math.max(0, Number(opts.timeoutMs || 0))

  return new Promise(function (resolve) {
    var settled = false
    var timer = scheduleTimeout(function () {
      finish({ authoritative: false, cursor: null })
    }, timeoutMs)

    function finish(result) {
      if (settled) return
      settled = true
      cancelTimeout(timer)
      resolve(result)
    }

    try {
      request().then(function (data) {
        var result = {
          authoritative: true,
          cursor: (data && data.video_id) ? data : null
        }
        var serverTimeMs = Math.max(0, Number(data && data.server_time_ms || 0))
        if (serverTimeMs > 0) result.serverTimeMs = serverTimeMs
        finish(result)
      }).catch(function () {
        finish({ authoritative: false, cursor: null })
      })
    } catch (_) {
      finish({ authoritative: false, cursor: null })
    }
  })
}

export function reconcileMomentsCursorRead(serverRead, localResume, scope) {
  if (!serverRead || !serverRead.authoritative) {
    return { apply: false, resume: localResume || null }
  }
  var cursor = serverRead.cursor
  if (!cursor || !cursor.video_id) return { apply: true, resume: null }

  var resume = {
    videoId: String(cursor.video_id),
    page: Math.max(1, parseInt(cursor.page, 10) || 1),
    index: Math.max(0, parseInt(cursor.index, 10) || 0),
    ts: Math.max(0, Number(cursor.updated_at_ms || 0)),
    scope: scope
  }
  var sortAtMs = Math.max(0, parseInt(cursor.sort_at_ms, 10) || 0)
  if (sortAtMs > 0) resume.sortAtMs = sortAtMs
  return { apply: true, resume: resume }
}

export function nextMomentsCursorTimestamp(correctedNowMs, currentResume) {
  var nowMs = Math.max(1, Math.floor(Number(correctedNowMs || 0)))
  var currentMs = Math.max(0, Math.floor(Number(currentResume && currentResume.ts || 0)))
  if (currentMs >= Number.MAX_SAFE_INTEGER) return Number.MAX_SAFE_INTEGER
  return Math.max(nowMs, currentMs + 1)
}

export function observeMomentsCursorTimestamp(clockByScope, scope, updatedAtMs) {
  var observed = Math.max(0, Math.floor(Number(updatedAtMs || 0)))
  var current = Math.max(0, Number(clockByScope.get(scope) || 0))
  clockByScope.set(scope, Math.max(current, observed))
}

export function advanceMomentsCursorTimestamp(clockByScope, scope, correctedNowMs, cachedResume) {
  var observed = Math.max(0, Number(clockByScope.get(scope) || 0))
  var cached = Math.max(0, Number(cachedResume && cachedResume.ts || 0))
  var updatedAtMs = nextMomentsCursorTimestamp(correctedNowMs, {
    ts: Math.max(observed, cached)
  })
  clockByScope.set(scope, updatedAtMs)
  return updatedAtMs
}

export function createMomentsCursorWriteQueue() {
  var statesByScope = new Map()

  function createState(scope) {
    var state = {
      scope: scope,
      active: 0,
      latest: null,
      drainWaiters: []
    }
    statesByScope.set(scope, state)
    return state
  }

  function finishDrain(state) {
    if (state.active > 0 || state.latest) return
    if (statesByScope.get(state.scope) === state) statesByScope.delete(state.scope)
    var waiters = state.drainWaiters.splice(0)
    waiters.forEach(function (resolve) { resolve() })
  }

  function startWrite(state, task) {
    state.active += 1
    var operation
    try {
      operation = Promise.resolve(task.write())
    } catch (error) {
      operation = Promise.reject(error)
    }
    operation.then(function (value) {
      task.waiters.forEach(function (waiter) { waiter.resolve(value) })
    }, function (error) {
      task.waiters.forEach(function (waiter) { waiter.reject(error) })
    }).then(function () {
      state.active -= 1
      if (state.active === 0 && state.latest) {
        var latest = state.latest
        state.latest = null
        startWrite(state, latest)
        return
      }
      finishDrain(state)
    })
  }

  return {
    enqueue: function (scope, write) {
      var state = statesByScope.get(scope) || createState(scope)
      var resolveResult
      var rejectResult
      var result = new Promise(function (resolve, reject) {
        resolveResult = resolve
        rejectResult = reject
      })
      var task = {
        write: write,
        waiters: [{ resolve: resolveResult, reject: rejectResult }]
      }
      if (state.active === 0) {
        startWrite(state, task)
        return result
      }
      if (state.latest) {
        task.waiters = state.latest.waiters.concat(task.waiters)
      }
      state.latest = task
      return result
    },
    afterWrites: function (scope, read) {
      var state = statesByScope.get(scope)
      if (!state || (state.active === 0 && !state.latest)) return Promise.resolve().then(read)
      return new Promise(function (resolve) {
        state.drainWaiters.push(resolve)
      }).then(read)
    },
    flushLatest: function () {
      statesByScope.forEach(function (state) {
        if (!state.latest) return
        var latest = state.latest
        state.latest = null
        startWrite(state, latest)
      })
    }
  }
}

export function momentsResumeVideoId(resume, legacyVideoId, scope) {
  var resumeId = String(resume && resume.videoId || '').trim()
  if (resumeId) return resumeId
  if (scope !== 'all') return ''
  return String(legacyVideoId || '').trim()
}
