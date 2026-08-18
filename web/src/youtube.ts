export function playerURL(embedUrl: string, playerId: string, short: boolean, origin = window.location.origin) {
  const url = new URL(embedUrl)
  url.searchParams.set('autoplay', '1')
  url.searchParams.set('playsinline', '1')
  url.searchParams.set('enablejsapi', '1')
  url.searchParams.set('origin', origin)
  url.searchParams.set('widget_referrer', origin)
  url.searchParams.set('playerapiid', playerId)
  if (short) {
    const videoId = url.pathname.split('/').filter(Boolean).at(-1)
    url.searchParams.set('loop', '1')
    if (videoId) url.searchParams.set('playlist', videoId)
  }
  return url.toString()
}
