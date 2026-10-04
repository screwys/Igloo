// Shaka's AAC parser accepts ID3 only before the first audio frame.
function spaceAudio(data) {
  const bytes = new Uint8Array(data)
  const parts = []
  let offset = 0
  let length = 0
  let removed = false
  while (offset < bytes.length) {
    let size
    let keep = true
    if (bytes[offset] === 0x49 && bytes[offset + 1] === 0x44 && bytes[offset + 2] === 0x33) {
      if (offset + 10 > bytes.length) return data
      for (let i = 6; i < 10; i++) {
        if (bytes[offset + i] & 0x80) return data
      }
      size = 10 + ((bytes[offset + 6] << 21) | (bytes[offset + 7] << 14) | (bytes[offset + 8] << 7) | bytes[offset + 9])
      if (bytes[offset + 3] === 4 && (bytes[offset + 5] & 0x10)) size += 10
      keep = offset === 0
    } else {
      if (offset + 7 > bytes.length || bytes[offset] !== 0xff || (bytes[offset + 1] & 0xf6) !== 0xf0) return data
      size = ((bytes[offset + 3] & 3) << 11) | (bytes[offset + 4] << 3) | (bytes[offset + 5] >> 5)
      if (size < ((bytes[offset + 1] & 1) ? 7 : 9)) return data
    }
    if (offset + size > bytes.length) return data
    if (keep) {
      parts.push(bytes.subarray(offset, offset + size))
      length += size
    } else {
      removed = true
    }
    offset += size
  }
  if (!removed) return data
  const audio = new Uint8Array(length)
  offset = 0
  for (const part of parts) {
    audio.set(part, offset)
    offset += part.length
  }
  return audio.buffer
}

export function configureXSpaceAudio(player, originalURL) {
  if (!/^https:\/\/(?:x|twitter)\.com\/i\/spaces\//.test(originalURL || '')) return
  player.getNetworkingEngine().registerResponseFilter(function (type, response) {
    if (type !== window.shaka.net.NetworkingEngine.RequestType.SEGMENT) return
    if ((response.headers['content-type'] || '').split(';')[0].trim().toLowerCase() !== 'audio/aac') return
    response.data = spaceAudio(response.data)
  })
}
