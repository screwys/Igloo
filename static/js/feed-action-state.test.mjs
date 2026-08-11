import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'

function extractFunction(source, name) {
  var start = source.indexOf('function ' + name + '(')
  assert.notEqual(start, -1, 'missing ' + name)
  var bodyStart = source.indexOf('{', start)
  var depth = 0
  for (var i = bodyStart; i < source.length; i++) {
    if (source[i] === '{') depth++
    if (source[i] === '}') depth--
    if (depth === 0) return source.slice(start, i + 1)
  }
  throw new Error('unterminated ' + name)
}

class FakeCard {
  constructor(tweetId, contentHash, bookmarked) {
    this.attributes = new Map([
      ['data-tweet-id', tweetId],
      ['data-content-hash', contentHash],
    ])
    this.dataset = { liked: '1', bookmarked: bookmarked ? '1' : '0' }
  }

  getAttribute(name) { return this.attributes.get(name) || null }
}

test('bookmark state reaches another projection of the same post', async () => {
  var source = await readFile(new URL('./src/feed/index.js', import.meta.url), 'utf8')
  var root = new FakeCard('same_post', 'root_hash', true)
  var threadCopy = new FakeCard('same_post', 'thread_hash', false)
  var relatedCopy = new FakeCard('related_post', 'root_hash', false)
  var cards = [root, threadCopy, relatedCopy]
  var context = vm.createContext({
    document: {
      querySelectorAll(selector) {
        return cards.filter(function (card) {
          return selector.includes('data-tweet-id="' + card.getAttribute('data-tweet-id') + '"') ||
            selector.includes('data-content-hash="' + card.getAttribute('data-content-hash') + '"')
        })
      },
    },
    cssEscape: (value) => value,
    stateBool: (card, key) => card.dataset[key] === '1',
    setStateBool: (card, key, value) => { card.dataset[key] = value ? '1' : '0' },
    syncFeedButtons: () => {},
  })
  vm.runInContext(extractFunction(source, 'syncSiblingCards') + '\nthis.syncSiblingCards = syncSiblingCards', context)

  context.syncSiblingCards(root)

  assert.equal(threadCopy.dataset.bookmarked, '1')
  assert.equal(relatedCopy.dataset.bookmarked, '1')
})
