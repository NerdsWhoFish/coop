import { describe, expect, it } from 'vitest'
import { playerURL } from './youtube'

describe('YouTube playback URL', () => {
  it('always autoplays through the iframe API', () => {
    const result = new URL(playerURL('https://www.youtube-nocookie.com/embed/abc?rel=0', 'player-1', false, 'https://coop.example'))

    expect(result.searchParams.get('autoplay')).toBe('1')
    expect(result.searchParams.get('playsinline')).toBe('1')
    expect(result.searchParams.get('enablejsapi')).toBe('1')
    expect(result.searchParams.get('playerapiid')).toBe('player-1')
  })

  it('loops Shorts without changing the regular player', () => {
    const result = new URL(playerURL('https://www.youtube-nocookie.com/embed/short-id?rel=0', 'short-player', true, 'https://coop.example'))

    expect(result.searchParams.get('loop')).toBe('1')
    expect(result.searchParams.get('playlist')).toBe('short-id')
  })
})
