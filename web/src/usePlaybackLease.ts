import { useEffect, useEffectEvent } from 'react'
import { api } from './api'

type PlaybackLeaseOptions = {
  videoId: string
  active: boolean
  onBlocked: () => void
  onError: (message: string) => void
}

export function usePlaybackLease({ videoId, active, onBlocked, onError }: PlaybackLeaseOptions) {
  const blocked = useEffectEvent(onBlocked)
  const failed = useEffectEvent(onError)

  useEffect(() => {
    if (!active) return

    let cancelled = false
    let heartbeat: number | undefined
    let startedAt: Date | undefined

    const stop = () => {
      if (heartbeat !== undefined) window.clearInterval(heartbeat)
      heartbeat = undefined
      if (!startedAt) return
      const segmentStart = startedAt
      startedAt = undefined
      void api.playback(videoId, 'stopped')
      void api.recordWatch(
        videoId,
        segmentStart.toISOString(),
        Math.max(0, Math.floor((Date.now() - segmentStart.getTime()) / 1000)),
      )
    }

    const block = () => {
      stop()
      blocked()
    }

    const start = async () => {
      if (cancelled || document.hidden || startedAt) return
      try {
        const result = await api.playback(videoId, 'started')
        if (cancelled || document.hidden) {
          if (result.allowed) void api.playback(videoId, 'stopped')
          return
        }
        if (!result.allowed) {
          block()
          return
        }
        startedAt = new Date()
        heartbeat = window.setInterval(() => {
          void api.playback(videoId, 'heartbeat')
            .then(result => { if (!result.allowed) block() })
            .catch(() => {})
        }, 15000)
      } catch (cause) {
        failed(cause instanceof Error ? cause.message : 'Coop could not report playback.')
      }
    }

    const visibilityChanged = () => {
      if (document.hidden) stop()
      else void start()
    }

    document.addEventListener('visibilitychange', visibilityChanged)
    void start()
    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', visibilityChanged)
      stop()
    }
  }, [active, videoId])
}
