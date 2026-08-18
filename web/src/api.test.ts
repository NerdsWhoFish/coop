import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import type { WebLink } from './types'

afterEach(() => vi.unstubAllGlobals())

describe('browser authentication boundary', () => {
  it('uses same-origin credentials without exposing a bearer token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      id: 'child', name: 'River', shortsEnabled: true, watchPageAutoplay: true,
      channelDiscoveryEnabled: true, webLinkingEnabled: true, allowSelfUnpair: false,
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await api.me()

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/child/me', expect.objectContaining({
      credentials: 'same-origin',
    }))
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(new Headers(init.headers).has('Authorization')).toBe(false)
  })

  it('keeps the browser-only link secret in a header instead of the URL', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      approved: false, expiresAt: '2026-08-17T20:00:00Z',
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const link: WebLink = {
      id: '15a88d97-44e7-43e7-b5ea-4c6f48271668', approvalToken: 'approval',
      redemptionToken: 'browser-only', expiresAt: '2026-08-17T20:00:00Z', qrPayload: 'coop://link',
    }

    await api.linkStatus(link)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).not.toContain('browser-only')
    expect(new Headers(init.headers).get('X-Coop-Link-Secret')).toBe('browser-only')
  })
})
