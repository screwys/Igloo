export function normalizeVolume(value, fallback) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    const fallbackValue = Number(fallback)
    return Number.isFinite(fallbackValue) ? Math.max(0, Math.min(1, fallbackValue)) : 1
  }
  return Math.max(0, Math.min(1, parsed))
}

export function readStoredVolume(storage, key, fallback) {
  try {
    const stored = storage && storage.getItem(key)
    if (stored == null || stored === '') return normalizeVolume(fallback, 1)
    return normalizeVolume(stored, fallback)
  } catch (_) {
    return normalizeVolume(fallback, 1)
  }
}

export function writeStoredVolume(storage, key, value) {
  const volume = normalizeVolume(value, 1)
  try {
    if (storage) storage.setItem(key, String(volume))
  } catch (_) {}
  return volume
}

export function volumeIconLevel(muted, value) {
  const volume = normalizeVolume(value, 0)
  if (muted || volume === 0) return 'muted'
  return volume < 0.5 ? 'low' : 'high'
}
