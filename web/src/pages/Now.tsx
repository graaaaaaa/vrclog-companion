import { useState, useEffect, useCallback, useRef } from 'react'
import { apiClient } from '../api/client'
import type { StateSnapshot } from '../api/types'
import { useSSE } from '../hooks/useSSE'
import { SafeLink } from '../components/SafeLink'
import { CopyButton } from '../components/CopyButton'

function statusLabel(status: 'observed' | 'failed'): string {
  return status === 'failed' ? '再生失敗' : 'URL検出'
}

function statusBadgeClass(status: 'observed' | 'failed'): string {
  return status === 'failed' ? 'bg-red-100 text-red-800' : 'bg-blue-100 text-blue-800'
}

function Now() {
  const [state, setState] = useState<StateSnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Refetching /api/v1/state on every observation avoids reimplementing
  // Projector logic client-side; the server is the single source of truth
  // for derived state.
  const refetchTimer = useRef<number | null>(null)
  const handleObservation = useCallback(() => {
    if (refetchTimer.current !== null) return
    refetchTimer.current = window.setTimeout(() => {
      refetchTimer.current = null
      apiClient
        .fetchState()
        .then(setState)
        .catch((err) => console.error('Failed to refetch state:', err))
    }, 150)
  }, [])

  const handleStateUpdate = useCallback((newState: StateSnapshot) => {
    setState(newState)
    setLoading(false)
    setError(null)
  }, [])

  const { connected, error: sseError, reconnecting } = useSSE({
    onObservation: handleObservation,
    onStateUpdate: handleStateUpdate,
  })

  useEffect(() => {
    apiClient
      .fetchState()
      .then((data) => {
        setState(data)
        setLoading(false)
      })
      .catch((err) => {
        setError(err.message)
        setLoading(false)
      })
  }, [])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500">Loading...</div>
      </div>
    )
  }

  if (error && !state) {
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-4">
        <p className="text-red-700">Error: {error}</p>
      </div>
    )
  }

  const media = state?.latest_openable_media

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2 text-sm">
        <span
          className={`w-2 h-2 rounded-full ${
            connected ? 'bg-green-500' : reconnecting ? 'bg-yellow-500' : 'bg-red-500'
          }`}
        />
        <span className="text-gray-600">
          {connected ? 'Connected' : reconnecting ? 'Reconnecting...' : 'Disconnected'}
        </span>
        {sseError && <span className="text-red-500">({sseError})</span>}
      </div>

      <div className="bg-white rounded-lg shadow p-4">
        <h2 className="text-lg font-semibold text-gray-800 mb-3">Current World</h2>
        {state?.world ? (
          <div className="space-y-1">
            <p className="text-gray-900 font-medium">{state.world.name || 'Unknown'}</p>
            <p className="text-sm text-gray-500">Instance: {state.world.instance_id || 'Unknown'}</p>
            <p className="text-xs text-gray-400">
              Joined: {new Date(state.world.joined_at).toLocaleString()}
            </p>
          </div>
        ) : (
          <p className="text-gray-500">Not in a world</p>
        )}
      </div>

      {media && (
        <div className="bg-white rounded-lg shadow p-4">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-lg font-semibold text-gray-800">Latest Media</h2>
            <span className={`px-2 py-0.5 rounded text-xs font-medium ${statusBadgeClass(media.status)}`}>
              {statusLabel(media.status)}
            </span>
          </div>
          <SafeLink url={media.url} className="text-sm text-blue-600 hover:underline break-all block mb-3" />
          <div className="flex gap-2">
            <CopyButton text={media.url} />
            <SafeLink
              url={media.url}
              className="px-3 py-1.5 text-sm rounded-md bg-blue-50 text-blue-700 hover:bg-blue-100 transition-colors"
            >
              ブラウザで開く
            </SafeLink>
            <a
              href="#/media"
              className="px-3 py-1.5 text-sm rounded-md text-gray-600 hover:bg-gray-100 transition-colors"
            >
              詳細
            </a>
          </div>
        </div>
      )}

      <div className="bg-white rounded-lg shadow p-4">
        <h2 className="text-lg font-semibold text-gray-800 mb-3">
          Players ({state?.players.length || 0})
        </h2>
        {state?.players && state.players.length > 0 ? (
          <ul className="divide-y divide-gray-100">
            {state.players.map((player, idx) => (
              <li key={player.id || player.display_name || idx} className="py-2">
                <div className="flex justify-between items-center">
                  <span className="text-gray-900">{player.display_name || 'Unknown'}</span>
                  <span className="text-xs text-gray-400">
                    {new Date(player.joined_at).toLocaleTimeString()}
                  </span>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-gray-500">No players</p>
        )}
      </div>
    </div>
  )
}

export default Now
