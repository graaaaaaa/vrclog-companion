import { useState, useEffect } from 'react'
import { apiClient, StatsResponse } from '../api/client'

function Stats() {
  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadStats()
  }, [])

  const loadStats = async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await apiClient.fetchStats()
      setStats(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load stats')
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500">Loading...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-4">
        <p className="text-red-700">Error: {error}</p>
        <button
          onClick={loadStats}
          className="mt-2 text-sm text-red-600 hover:text-red-800 underline"
        >
          Retry
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header with refresh */}
      <div className="flex justify-between items-center">
        <h1 className="text-xl font-semibold text-gray-800">Today's Stats</h1>
        <button
          onClick={loadStats}
          className="text-sm text-blue-600 hover:text-blue-800"
        >
          Refresh
        </button>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Joins */}
        <div className="bg-white rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium text-gray-500">Joins</h3>
            <span className="text-2xl font-bold text-green-600">
              {stats?.today_joins ?? 0}
            </span>
          </div>
        </div>

        {/* Leaves */}
        <div className="bg-white rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium text-gray-500">Leaves</h3>
            <span className="text-2xl font-bold text-red-600">
              {stats?.today_leaves ?? 0}
            </span>
          </div>
        </div>

        {/* World Changes */}
        <div className="bg-white rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium text-gray-500">World Changes</h3>
            <span className="text-2xl font-bold text-blue-600">
              {stats?.today_world_changes ?? 0}
            </span>
          </div>
        </div>
      </div>

      {/* Recent Players */}
      <div className="bg-white rounded-lg shadow p-4">
        <h2 className="text-lg font-semibold text-gray-800 mb-3">Recent Players</h2>
        {stats?.recent_players && stats.recent_players.length > 0 ? (
          <ul className="divide-y divide-gray-100">
            {stats.recent_players.map((player, idx) => (
              <li key={idx} className="py-2">
                <span className="text-gray-900">{player}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-gray-500">No recent players</p>
        )}
      </div>

      {/* Last Event */}
      {stats?.last_event_at && (
        <div className="text-sm text-gray-500 text-center">
          Last event: {new Date(stats.last_event_at).toLocaleString()}
        </div>
      )}
    </div>
  )
}

export default Stats
