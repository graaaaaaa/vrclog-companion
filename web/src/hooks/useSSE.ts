import { useEffect, useRef, useCallback, useState } from 'react'
import { apiClient } from '../api/client'
import type { Observation, StateSnapshot } from '../api/types'

interface UseSSEOptions {
  onObservation?: (observation: Observation) => void
  onStateUpdate?: (state: StateSnapshot) => void
  enabled?: boolean
}

interface UseSSEResult {
  connected: boolean
  error: string | null
  reconnecting: boolean
}

const TOKEN_REFRESH_INTERVAL = 4 * 60 * 1000 // 4 minutes (token expires at 5)
const MAX_BACKOFF = 30000 // 30 seconds
const INITIAL_BACKOFF = 1000 // 1 second

// useSSE subscribes to the generic /api/v1/stream "observation" event.
// The server emits every Observation as a single event type — there is no
// per-EventKind event name — so callers that need derived state should
// prefer onStateUpdate (triggered on resync) and refetch /api/v1/state
// after onObservation fires, rather than reimplementing projection logic
// client-side.
export function useSSE(options: UseSSEOptions): UseSSEResult {
  const { onObservation, onStateUpdate, enabled = true } = options
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reconnecting, setReconnecting] = useState(false)

  const eventSourceRef = useRef<EventSource | null>(null)
  const tokenRef = useRef<string | null>(null)
  const tokenRefreshTimerRef = useRef<number | null>(null)
  const reconnectTimerRef = useRef<number | null>(null)
  const backoffRef = useRef(INITIAL_BACKOFF)
  const lastEventIdRef = useRef<string | null>(null)

  const cleanup = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
      eventSourceRef.current = null
    }
    if (tokenRefreshTimerRef.current) {
      clearTimeout(tokenRefreshTimerRef.current)
      tokenRefreshTimerRef.current = null
    }
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
  }, [])

  const fetchAndSetToken = useCallback(async (): Promise<boolean> => {
    try {
      const tokenRes = await apiClient.fetchToken()
      tokenRef.current = tokenRes.token
      return true
    } catch (err) {
      console.error('Failed to fetch token:', err)
      return false
    }
  }, [])

  const resync = useCallback(async () => {
    try {
      const state = await apiClient.fetchState()
      onStateUpdate?.(state)
    } catch (err) {
      console.error('Failed to resync state:', err)
    }
  }, [onStateUpdate])

  const connect = useCallback(async () => {
    cleanup()
    setError(null)

    const hasToken = await fetchAndSetToken()
    if (!hasToken) {
      // No EventSource exists yet at this point, so es.onerror's backoff
      // reconnect never fires -- without scheduling a retry here, a token
      // fetch failing during the startup readiness-gate 503 window would
      // leave SSE permanently disconnected until a manual page reload.
      setError('Failed to authenticate')
      setReconnecting(true)
      const delay = backoffRef.current
      backoffRef.current = Math.min(backoffRef.current * 2, MAX_BACKOFF)
      reconnectTimerRef.current = window.setTimeout(() => {
        connect()
      }, delay)
      return
    }

    // Resync full projected state before connecting, so the UI is never
    // stale between page load and the first live observation.
    await resync()

    const params = new URLSearchParams({ token: tokenRef.current! })
    if (lastEventIdRef.current) {
      params.set('last_event_id', lastEventIdRef.current)
    }
    const url = `/api/v1/stream?${params.toString()}`
    const es = new EventSource(url)
    eventSourceRef.current = es

    es.onopen = () => {
      setConnected(true)
      setReconnecting(false)
      setError(null)
      backoffRef.current = INITIAL_BACKOFF

      tokenRefreshTimerRef.current = window.setTimeout(async () => {
        const refreshed = await fetchAndSetToken()
        if (refreshed) {
          connect()
        }
      }, TOKEN_REFRESH_INTERVAL)
    }

    es.addEventListener('observation', (msg: MessageEvent) => {
      if (msg.lastEventId) {
        lastEventIdRef.current = msg.lastEventId
      }
      try {
        const observation = JSON.parse(msg.data) as Observation
        onObservation?.(observation)
      } catch (err) {
        console.error('Failed to parse SSE observation:', err)
      }
    })

    // A reset event means the server could not resolve our Last-Event-ID
    // (e.g. the database was reset). Drop our bookmark and do a full
    // resync rather than looping on the same stale ID.
    es.addEventListener('reset', () => {
      lastEventIdRef.current = null
      resync()
    })

    es.onerror = () => {
      setConnected(false)
      cleanup()

      setReconnecting(true)
      const delay = backoffRef.current
      backoffRef.current = Math.min(backoffRef.current * 2, MAX_BACKOFF)

      reconnectTimerRef.current = window.setTimeout(() => {
        connect()
      }, delay)
    }
  }, [cleanup, fetchAndSetToken, resync, onObservation])

  useEffect(() => {
    if (enabled) {
      connect()
    } else {
      cleanup()
      setConnected(false)
    }

    return cleanup
  }, [enabled, connect, cleanup])

  return { connected, error, reconnecting }
}
