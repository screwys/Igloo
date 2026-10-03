export async function playVideo(video, shouldPlay = () => true) {
  let owner = video.ownerDocument
  try { owner = owner.defaultView.top.document } catch (_) {}
  function watchingOtherVideo() {
    const fullscreen = owner.fullscreenElement || owner.webkitFullscreenElement
    return fullscreen && !fullscreen.contains(video)
  }
  if (watchingOtherVideo()) {
    await new Promise(function (resolve) {
      function fullscreenChanged() {
        if (watchingOtherVideo()) return
        owner.removeEventListener('fullscreenchange', fullscreenChanged)
        owner.removeEventListener('webkitfullscreenchange', fullscreenChanged)
        resolve()
      }
      owner.addEventListener('fullscreenchange', fullscreenChanged)
      owner.addEventListener('webkitfullscreenchange', fullscreenChanged)
    })
  }
  if (video.isConnected && shouldPlay()) await video.play()
}
