// One local details panel serves feed cards, profile cards, and loaded fragments.
var panel = null
var trigger = null
var closeTimer = null

function cancelAccountDetailsClose() {
  clearTimeout(closeTimer)
  closeTimer = null
}

function scheduleAccountDetailsClose() {
  cancelAccountDetailsClose()
  closeTimer = setTimeout(closeAccountDetails, 160)
}

function closeAccountDetails() {
  cancelAccountDetailsClose()
  if (panel && panel.matches(':popover-open')) panel.hidePopover()
}

function openAccountDetails(button, focus) {
  cancelAccountDetailsClose()
  if (trigger === button && panel && panel.matches(':popover-open')) {
    if (focus) panel.querySelector('[data-account-details-close]').focus({ preventScroll: true })
    return
  }
  var content = button.parentElement.querySelector('[data-account-details-content]')
  if (!content) return
  closeAccountDetails()
  if (!panel) {
    panel = document.createElement('div')
    panel.className = 'account-details-panel'
    panel.setAttribute('popover', 'auto')
    panel.setAttribute('role', 'dialog')
    panel.addEventListener('beforetoggle', function (event) {
      if (event.newState === 'closed' && trigger) trigger.setAttribute('aria-expanded', 'false')
    })
    panel.addEventListener('pointerenter', cancelAccountDetailsClose)
    panel.addEventListener('pointerleave', scheduleAccountDetailsClose)
    panel.addEventListener('focusin', cancelAccountDetailsClose)
    document.body.appendChild(panel)
  }
  trigger = button
  panel.setAttribute('aria-label', button.getAttribute('aria-label'))
  panel.replaceChildren(content.content.cloneNode(true))
  panel.showPopover()
  button.setAttribute('aria-expanded', 'true')
  var anchor = button.getBoundingClientRect()
  var bounds = panel.getBoundingClientRect()
  panel.style.left = Math.max(8, Math.min(anchor.left, window.innerWidth - bounds.width - 8)) + 'px'
  var below = anchor.bottom + 6
  panel.style.top = Math.max(8, below + bounds.height <= window.innerHeight - 8 ? below : anchor.top - bounds.height - 6) + 'px'
  if (focus) panel.querySelector('[data-account-details-close]').focus({ preventScroll: true })
}

document.addEventListener('pointerover', function (event) {
  if (event.pointerType === 'touch') return
  var button = event.target.closest('[data-account-details]')
  if (button) openAccountDetails(button, false)
})

document.addEventListener('pointerout', function (event) {
  var button = event.target.closest('[data-account-details]')
  if (button && !button.contains(event.relatedTarget)) scheduleAccountDetailsClose()
})

document.addEventListener('click', function (event) {
  if (event.target.closest('[data-account-details-close]')) {
    var previous = trigger
    closeAccountDetails()
    if (previous && previous.isConnected) previous.focus()
    return
  }
  var button = event.target.closest('[data-account-details]')
  if (!button) return
  event.preventDefault()
  openAccountDetails(button, true)
})

window.addEventListener('resize', closeAccountDetails)
window.addEventListener('popstate', closeAccountDetails)
document.addEventListener('scroll', function (event) {
  if (panel && !panel.contains(event.target)) closeAccountDetails()
}, true)
