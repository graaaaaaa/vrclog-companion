import { useState, useEffect, useCallback } from 'react'
import { apiClient, NowResponse, Event, PlayerInfo } from '../api/client'
import { useSSE } from '../hooks/useSSE'

function Now() {
  const [state, setState] = useState<NowResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const handleEvent = useCallback((event: Event) => {
    setState((prev) => {
      if (!prev) return prev

      switch (event.type) {
        case 'world_join':
          return {
            world: {
              WorldID: event.world_id || '',
              WorldName: event.world_name || '',
              InstanceID: event.instance_id || '',
              JoinedAt: event.ts,
            },
            players: [],
          }
        case 'player_join':
          const newPlayer: PlayerInfo = {
            PlayerName: event.player_name || '',
            PlayerID: event.player_id || '',
            JoinedAt: event.ts,
          }
          // Dedupe by PlayerID
          const existingIndex = prev.players.findIndex(
            (p) => (p.PlayerID && p.PlayerID === newPlayer.PlayerID) ||
                   (!p.PlayerID && p.PlayerName === newPlayer.PlayerName)
          )
          if (existingIndex >= 0) {
            return prev
          }
          return { ...prev, players: [...prev.players, newPlayer] }
        case 'player_left':
          return {
            ...prev,
            players: prev.players.filter(
              (p) => !(
                (event.player_id && p.PlayerID === event.player_id) ||
                (!event.player_id && p.PlayerName === event.player_name)
              )
            ),
          }
        default:
          return prev
      }
    })
  }, [])

  const handleStateUpdate = useCallback((newState: NowResponse) => {
    setState(newState)
    setLoading(false)
    setError(null)
  }, [])

  const { connected, error: sseError, reconnecting } = useSSE({
    onEvent: handleEvent,
    onStateUpdate: handleStateUpdate,
  })

  useEffect(() => {
    // Initial fetch
    apiClient
      .fetchNow()
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

  return (
    <div className="space-y-6">
      {/* Connection status */}
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

      {/* Current World */}
      <div className="bg-white rounded-lg shadow p-4">
        <h2 className="text-lg font-semibold text-gray-800 mb-3">Current World</h2>
        {state?.world ? (
          <div className="space-y-1">
            <p className="text-gray-900 font-medium">{state.world.WorldName || 'Unknown'}</p>
            <p className="text-sm text-gray-500">
              Instance: {state.world.InstanceID || 'Unknown'}
            </p>
            <p className="text-xs text-gray-400">
              Joined: {new Date(state.world.JoinedAt).toLocaleString()}
            </p>
          </div>
        ) : (
          <p className="text-gray-500">Not in a world</p>
        )}
      </div>

      {/* Players */}
      <div className="bg-white rounded-lg shadow p-4">
        <h2 className="text-lg font-semibold text-gray-800 mb-3">
          Players ({state?.players.length || 0})
        </h2>
        {state?.players && state.players.length > 0 ? (
          <ul className="divide-y divide-gray-100">
            {state.players.map((player, idx) => (
              <li key={player.PlayerID || player.PlayerName || idx} className="py-2">
                <div className="flex justify-between items-center">
                  <span className="text-gray-900">{player.PlayerName || 'Unknown'}</span>
                  <span className="text-xs text-gray-400">
                    {new Date(player.JoinedAt).toLocaleTimeString()}
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
