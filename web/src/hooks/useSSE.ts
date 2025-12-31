import { useEffect, useRef, useCallback, useState } from 'react'
import { apiClient, NowResponse, Event } from '../api/client'

interface UseSSEOptions {
  onEvent?: (event: Event) => void
  onStateUpdate?: (state: NowResponse) => void
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

export function useSSE(options: UseSSEOptions): UseSSEResult {
  const { onEvent, onStateUpdate, enabled = true } = options
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reconnecting, setReconnecting] = useState(false)

  const eventSourceRef = useRef<EventSource | null>(null)
  const tokenRef = useRef<string | null>(null)
  const tokenRefreshTimerRef = useRef<number | null>(null)
  const reconnectTimerRef = useRef<number | null>(null)
  const backoffRef = useRef(INITIAL_BACKOFF)

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
      const state = await apiClient.fetchNow()
      onStateUpdate?.(state)
    } catch (err) {
      console.error('Failed to resync state:', err)
    }
  }, [onStateUpdate])

  const connect = useCallback(async () => {
    cleanup()
    setError(null)

    // Fetch initial token
    const hasToken = await fetchAndSetToken()
    if (!hasToken) {
      setError('Failed to authenticate')
      return
    }

    // Resync state before connecting
    await resync()

    // Connect to SSE with token
    const url = `/api/v1/stream?token=${encodeURIComponent(tokenRef.current!)}`
    const es = new EventSource(url)
    eventSourceRef.current = es

    es.onopen = () => {
      setConnected(true)
      setReconnecting(false)
      setError(null)
      backoffRef.current = INITIAL_BACKOFF

      // Schedule token refresh
      tokenRefreshTimerRef.current = window.setTimeout(async () => {
        const refreshed = await fetchAndSetToken()
        if (refreshed) {
          // Reconnect with new token
          connect()
        }
      }, TOKEN_REFRESH_INTERVAL)
    }

    es.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data) as Event
        onEvent?.(event)
      } catch (err) {
        console.error('Failed to parse SSE message:', err)
      }
    }

    es.onerror = () => {
      setConnected(false)
      cleanup()

      // Schedule reconnect with exponential backoff
      setReconnecting(true)
      const delay = backoffRef.current
      backoffRef.current = Math.min(backoffRef.current * 2, MAX_BACKOFF)

      reconnectTimerRef.current = window.setTimeout(() => {
        connect()
      }, delay)
    }
  }, [cleanup, fetchAndSetToken, resync, onEvent])

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
