// @vitest-environment jsdom

import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parentBlockedPlaybackMessage, usePlaybackLease } from './usePlaybackLease'

const mocks = vi.hoisted(() => ({
  playback: vi.fn(),
  recordWatch: vi.fn(),
}))

vi.mock('./api', () => ({ api: mocks }))

describe('browser playback lease', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mocks.playback.mockReset().mockResolvedValue({ allowed: true })
    mocks.recordWatch.mockReset().mockResolvedValue(undefined)
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  })

  afterEach(() => vi.useRealTimers())

  it('stops reporting and records the segment when the tab becomes hidden', async () => {
    renderHook(() => usePlaybackLease({
      videoId: 'video-1', active: true, onBlocked: vi.fn(), onError: vi.fn(),
    }))
    await act(async () => { await Promise.resolve() })
    expect(mocks.playback).toHaveBeenCalledWith('video-1', 'started')

    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    act(() => document.dispatchEvent(new Event('visibilitychange')))

    expect(mocks.playback).toHaveBeenCalledWith('video-1', 'stopped')
    expect(mocks.recordWatch).toHaveBeenCalledWith('video-1', expect.any(String), expect.any(Number))
  })

  it('immediately removes playback when a heartbeat says the video was blocked', async () => {
    const blocked = vi.fn()
    mocks.playback
      .mockResolvedValueOnce({ allowed: true })
      .mockResolvedValueOnce({ allowed: false })
      .mockResolvedValue({ allowed: true })
    renderHook(() => usePlaybackLease({
      videoId: 'video-2', active: true, onBlocked: blocked, onError: vi.fn(),
    }))
    await act(async () => { await Promise.resolve() })

    await act(async () => { await vi.advanceTimersByTimeAsync(15000) })

    expect(blocked).toHaveBeenCalledWith(parentBlockedPlaybackMessage)
    expect(mocks.playback).toHaveBeenCalledWith('video-2', 'stopped')
  })
})
