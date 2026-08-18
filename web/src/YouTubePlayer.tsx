import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { LoaderCircle, Volume2 } from 'lucide-react'
import { playerURL } from './youtube'

type YouTubePlayerProps = {
  embedUrl: string
  title: string
  thumbnailUrl?: string
  short?: boolean
}

export function YouTubePlayer({ embedUrl, title, thumbnailUrl, short = false }: YouTubePlayerProps) {
  const frame = useRef<HTMLIFrameElement>(null)
  const playerId = useId().replaceAll(':', '')
  const [ready, setReady] = useState(false)
  const [mutedAutoplay, setMutedAutoplay] = useState(false)
  const src = useMemo(() => playerURL(embedUrl, playerId, short), [embedUrl, playerId, short])

  useEffect(() => {
    const reveal = window.setTimeout(() => setReady(true), 12000)
    let fallback: number | undefined
    let playing = false
    const command = (func: string) => frame.current?.contentWindow?.postMessage(
      JSON.stringify({ event: 'command', func, args: [], id: playerId }),
      'https://www.youtube-nocookie.com',
    )
    const receive = (event: MessageEvent) => {
      if (event.origin !== 'https://www.youtube-nocookie.com' || event.source !== frame.current?.contentWindow) return
      try {
        const payload = typeof event.data === 'string' ? JSON.parse(event.data) as { event?: string; info?: { playerState?: number; videoLoadedFraction?: number } } : event.data
        if (payload?.event === 'onReady') {
          setReady(true)
          fallback = window.setTimeout(() => {
            if (playing) return
            command('mute')
            command('playVideo')
            setMutedAutoplay(true)
          }, 750)
        }
        if (payload?.info?.playerState === 1) {
          playing = true
          if (fallback !== undefined) window.clearTimeout(fallback)
          setReady(true)
        }
        if ((payload?.info?.videoLoadedFraction ?? 0) > 0) setReady(true)
      } catch {
        // YouTube also emits non-JSON messages; they are unrelated to player readiness.
      }
    }
    window.addEventListener('message', receive)
    return () => {
      window.clearTimeout(reveal)
      if (fallback !== undefined) window.clearTimeout(fallback)
      window.removeEventListener('message', receive)
    }
  }, [playerId, src])

  function unmute() {
    frame.current?.contentWindow?.postMessage(
      JSON.stringify({ event: 'command', func: 'unMute', args: [], id: playerId }),
      'https://www.youtube-nocookie.com',
    )
    setMutedAutoplay(false)
  }

  return <div className={`youtube-player${ready ? ' ready' : ''}`} style={thumbnailUrl ? { backgroundImage: `url(${thumbnailUrl})` } : undefined}>
    <iframe
      ref={frame}
      id={playerId}
      src={src}
      title={title}
      allow="autoplay; encrypted-media; picture-in-picture"
      allowFullScreen
      onLoad={() => frame.current?.contentWindow?.postMessage(JSON.stringify({ event: 'listening', id: playerId }), 'https://www.youtube-nocookie.com')}
    />
    {!ready && <div className="player-readying" aria-live="polite"><LoaderCircle className="spin" /><span>Getting the video ready…</span></div>}
    {mutedAutoplay && <button className="player-unmute" onClick={unmute}><Volume2 /> Tap for sound</button>}
  </div>
}
