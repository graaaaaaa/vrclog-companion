import { useState, useEffect, useCallback } from 'react'
import { apiClient, Event } from '../api/client'

type EventType = 'player_join' | 'player_left' | 'world_join' | ''

function History() {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [cursor, setCursor] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [filter, setFilter] = useState<EventType>('')

  const fetchEvents = useCallback(
    async (reset = false) => {
      setLoading(true)
      setError(null)

      try {
        const res = await apiClient.fetchEvents({
          type: filter || undefined,
          cursor: reset ? undefined : cursor || undefined,
          limit: 50,
        })

        if (reset) {
          setEvents(res.items)
        } else {
          setEvents((prev) => [...prev, ...res.items])
        }
        setCursor(res.next_cursor)
        setHasMore(res.next_cursor !== null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    },
    [filter, cursor]
  )

  useEffect(() => {
    // Reset and fetch when filter changes
    setCursor(null)
    setEvents([])
    fetchEvents(true)
  }, [filter]) // eslint-disable-line react-hooks/exhaustive-deps

  const loadMore = () => {
    if (!loading && hasMore) {
      fetchEvents()
    }
  }

  const getEventTypeLabel = (type: string) => {
    switch (type) {
      case 'player_join':
        return 'Join'
      case 'player_left':
        return 'Left'
      case 'world_join':
        return 'World'
      default:
        return type
    }
  }

  const getEventTypeColor = (type: string) => {
    switch (type) {
      case 'player_join':
        return 'bg-green-100 text-green-800'
      case 'player_left':
        return 'bg-red-100 text-red-800'
      case 'world_join':
        return 'bg-blue-100 text-blue-800'
      default:
        return 'bg-gray-100 text-gray-800'
    }
  }

  const getEventDescription = (event: Event) => {
    switch (event.type) {
      case 'player_join':
      case 'player_left':
        return event.player_name || 'Unknown player'
      case 'world_join':
        return event.world_name || 'Unknown world'
      default:
        return ''
    }
  }

  return (
    <div className="space-y-4">
      {/* Filter */}
      <div className="flex gap-2">
        <button
          onClick={() => setFilter('')}
          className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
            filter === '' ? 'bg-blue-100 text-blue-700' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
          }`}
        >
          All
        </button>
        <button
          onClick={() => setFilter('player_join')}
          className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
            filter === 'player_join' ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
          }`}
        >
          Joins
        </button>
        <button
          onClick={() => setFilter('player_left')}
          className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
            filter === 'player_left' ? 'bg-red-100 text-red-700' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
          }`}
        >
          Leaves
        </button>
        <button
          onClick={() => setFilter('world_join')}
          className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
            filter === 'world_join' ? 'bg-blue-100 text-blue-700' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
          }`}
        >
          Worlds
        </button>
      </div>

      {/* Error */}
      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-red-700">Error: {error}</p>
        </div>
      )}

      {/* Events list */}
      <div className="bg-white rounded-lg shadow">
        {events.length === 0 && !loading ? (
          <div className="p-4 text-center text-gray-500">No events found</div>
        ) : (
          <ul className="divide-y divide-gray-100">
            {events.map((event) => (
              <li key={event.id} className="p-3 flex items-center gap-3">
                <span
                  className={`px-2 py-0.5 rounded text-xs font-medium ${getEventTypeColor(
                    event.type
                  )}`}
                >
                  {getEventTypeLabel(event.type)}
                </span>
                <span className="flex-1 text-gray-900">{getEventDescription(event)}</span>
                <span className="text-xs text-gray-400">
                  {new Date(event.ts).toLocaleString()}
                </span>
              </li>
            ))}
          </ul>
        )}

        {/* Load more */}
        {hasMore && (
          <div className="p-3 border-t border-gray-100">
            <button
              onClick={loadMore}
              disabled={loading}
              className="w-full py-2 text-sm text-blue-600 hover:bg-blue-50 rounded-md disabled:opacity-50"
            >
              {loading ? 'Loading...' : 'Load more'}
            </button>
          </div>
        )}
      </div>

      {/* Loading indicator */}
      {loading && events.length === 0 && (
        <div className="flex items-center justify-center py-8">
          <div className="text-gray-500">Loading...</div>
        </div>
      )}
    </div>
  )
}

export default History
