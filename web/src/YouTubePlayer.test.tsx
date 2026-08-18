// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { YouTubePlayer } from './YouTubePlayer'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('YouTube autoplay', () => {
  it('falls back to muted playback when audible autoplay is blocked', () => {
    vi.useFakeTimers()
    render(<YouTubePlayer embedUrl="https://www.youtube-nocookie.com/embed/abc" title="Test video" />)
    const frame = screen.getByTitle('Test video') as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')

    fireEvent.load(frame)
    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://www.youtube-nocookie.com',
      source: frame.contentWindow,
      data: JSON.stringify({ event: 'onReady' }),
    }))
    act(() => vi.advanceTimersByTime(750))

    expect(postMessage).toHaveBeenCalledWith(expect.stringContaining('"func":"mute"'), 'https://www.youtube-nocookie.com')
    expect(postMessage).toHaveBeenCalledWith(expect.stringContaining('"func":"playVideo"'), 'https://www.youtube-nocookie.com')
    expect(screen.getByRole('button', { name: /tap for sound/i })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /tap for sound/i }))
    expect(postMessage).toHaveBeenCalledWith(expect.stringContaining('"func":"unMute"'), 'https://www.youtube-nocookie.com')
    expect(screen.queryByRole('button', { name: /tap for sound/i })).not.toBeInTheDocument()
  })

  it('keeps audible autoplay when the browser starts playback', () => {
    vi.useFakeTimers()
    render(<YouTubePlayer embedUrl="https://www.youtube-nocookie.com/embed/abc" title="Test video" />)
    const frame = screen.getByTitle('Test video') as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')

    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://www.youtube-nocookie.com',
      source: frame.contentWindow,
      data: JSON.stringify({ event: 'onReady' }),
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://www.youtube-nocookie.com',
      source: frame.contentWindow,
      data: JSON.stringify({ info: { playerState: 1 } }),
    }))
    act(() => vi.advanceTimersByTime(750))

    expect(postMessage).not.toHaveBeenCalledWith(expect.stringContaining('"func":"mute"'), expect.anything())
    expect(screen.queryByRole('button', { name: /tap for sound/i })).not.toBeInTheDocument()
  })
})
