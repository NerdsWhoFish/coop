export interface ChildProfile {
  id: string
  name: string
  avatarId?: string
  shortsEnabled: boolean
  watchPageAutoplay: boolean
  videoSearchTiles?: boolean
  channelDiscoveryEnabled: boolean
  webLinkingEnabled: boolean
  allowSelfUnpair: boolean
}

export interface Video {
  id: string
  channelId: string
  channelTitle?: string
  title: string
  thumbnailUrl?: string
  durationSeconds: number
  publishedAt: string
  isShort: boolean
  locked?: boolean
}

export interface Channel {
  id: string
  title: string
  description?: string
  thumbnailUrl?: string
  bannerUrl?: string
  subscriberCount?: number
  state?: 'allowed' | 'requestable'
  pendingRequest?: boolean
}

export interface Discovery {
  video: Video
  reason: string
  pendingRequest: boolean
}

export interface ChannelPage {
  channel: Channel
  state: 'allowed' | 'requestable'
  subscribed: boolean
  pendingRequest?: boolean
  videos: Video[]
}

export interface SearchResults { channels: Channel[]; videos: Video[] }
export interface WatchPage {
  video: Video
  embedUrl: string
  autoplay: boolean
  reaction?: 'like' | 'dislike'
  shareUrl: string
}

export interface ChildRequest {
  id: string
  channel: Channel
  status: 'pending' | 'approved' | 'denied'
  createdAt: string
}

export interface WebLink {
  id: string
  approvalToken: string
  redemptionToken: string
  expiresAt: string
  qrPayload: string
}
