import type {
  Channel, ChannelPage, ChildProfile, ChildRequest, Discovery, SearchResults, Video,
  WatchPage, WebLink,
} from './types'

export class APIError extends Error {
  constructor(public status: number, message: string, public code?: string) {
    super(message)
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    ...init,
    headers: {
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...init.headers,
    },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { message?: string; code?: string }
    throw new APIError(response.status, body.message ?? 'Coop could not finish that.', body.code)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

const json = (body: unknown): RequestInit => ({ method: 'POST', body: JSON.stringify(body) })

export const api = {
  me: () => request<ChildProfile>('/child/me'),
  feed: () => request<{ items: Video[] }>('/child/feed?limit=60').then(x => x.items),
  discovery: () => request<{ items: Discovery[] }>('/child/discovery?limit=12').then(x => x.items),
  shorts: (session: string, offset = 0, limit = 8) => request<{ items: Video[] }>(`/child/shorts?limit=${limit}&offset=${offset}&session=${encodeURIComponent(session)}`).then(x => x.items),
  subscriptions: () => request<Channel[]>('/child/subscriptions'),
  channel: (id: string) => request<ChannelPage>(`/child/channels/${encodeURIComponent(id)}`),
  search: (query: string, channelId?: string) => request<SearchResults>(
    `/child/search?q=${encodeURIComponent(query)}${channelId ? `&channelId=${encodeURIComponent(channelId)}` : ''}`,
  ),
  watch: (id: string) => request<WatchPage>(`/child/videos/${encodeURIComponent(id)}`),
  requests: () => request<ChildRequest[]>('/child/requests'),
  subscribe: (id: string, value: boolean) => request<void>(`/child/subscriptions/${encodeURIComponent(id)}`, { method: value ? 'PUT' : 'DELETE' }),
  react: (id: string, kind?: 'like' | 'dislike') => request<void>(`/child/videos/${encodeURIComponent(id)}/reaction`, kind ? { method: 'PUT', body: JSON.stringify({ kind }) } : { method: 'DELETE' }),
  ask: (channelId: string, promptedByVideoId?: string) => request<ChildRequest>('/child/requests', json({ channelId, promptedByVideoId })),
  playback: (videoId: string, state: 'started' | 'heartbeat' | 'stopped') => request<{ allowed: boolean }>('/child/playback', { method: 'PUT', body: JSON.stringify({ videoId, state }) }),
  recordWatch: (videoId: string, startedAt: string, secondsWatched: number) => request<void>(`/child/videos/${encodeURIComponent(videoId)}/watch`, json({ startedAt, secondsWatched })),
  createLink: (deviceName: string) => request<WebLink>('/web/link', json({ deviceName })),
  linkStatus: (link: WebLink) => request<{ approved: boolean; expiresAt: string }>(`/web/link/${link.id}`, { headers: { 'X-Coop-Link-Secret': link.redemptionToken } }),
  redeemLink: (link: WebLink) => request<ChildProfile>(`/web/link/${link.id}/redeem`, { method: 'POST', headers: { 'X-Coop-Link-Secret': link.redemptionToken } }),
  pair: (code: string, deviceName: string) => request<ChildProfile>('/web/pair', json({ code, deviceName })),
  logout: () => request<void>('/child/session', { method: 'DELETE' }),
}
