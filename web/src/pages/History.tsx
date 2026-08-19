import { useState, useEffect, useCallback } from 'react'
import { apiClient } from '../api/client'
import type { Observation } from '../api/types'

function typeBadgeClass(type: string): string {
  if (type.startsWith('player.joined')) return 'bg-green-100 text-green-800'
  if (type.startsWith('player.left')) return 'bg-red-100 text-red-800'
  if (type.startsWith('world.')) return 'bg-blue-100 text-blue-800'
  if (type.startsWith('resource.') || type.startsWith('media.')) return 'bg-purple-100 text-purple-800'
  return 'bg-gray-100 text-gray-800'
}

function summarize(obs: Observation): string {
  const p = obs.payload as Record<string, unknown>
  switch (obs.type) {
    case 'player.joined':
    case 'player.left': {
      const player = p.player as Record<string, unknown> | undefined
      return (player?.display_name as string) || 'Unknown player'
    }
    case 'world.joining_observed':
    case 'world.entering_observed': {
      const world = p.world as Record<string, unknown> | undefined
      return (world?.name as string) || (world?.id as string) || 'Unknown world'
    }
    default:
      return obs.type
  }
}

function ObservationRow({ obs }: { obs: Observation }) {
  return (
    <li className="p-3">
      <div className="flex items-center gap-3">
        <span className={`px-2 py-0.5 rounded text-xs font-medium ${typeBadgeClass(obs.type)}`}>
          {obs.type}
        </span>
        <span className="text-xs text-gray-400">{obs.adapter_id}</span>
        <span className="flex-1 text-gray-900 truncate">{summarize(obs)}</span>
        <span className="text-xs text-gray-400 whitespace-nowrap">
          {new Date(obs.occurred_at).toLocaleString()}
        </span>
      </div>
      <details className="mt-2">
        <summary className="cursor-pointer text-xs text-gray-500">payload</summary>
        {/* Rendered as plain text via JSON.stringify — never
            dangerouslySetInnerHTML — so an adversarial payload can only
            ever appear as inert text. */}
        <pre className="mt-1 p-2 bg-gray-50 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all">
          {JSON.stringify(obs.payload, null, 2)}
        </pre>
        <p className="mt-1 text-xs text-gray-400">
          record: {obs.record.source_id} @offset {obs.record.offset} line {obs.record.line}
        </p>
      </details>
    </li>
  )
}

// maxRetainedObservations caps how many rows History keeps in memory across
// repeated "Load more" clicks. Observations are newest-first, so capping
// keeps the newest rows and drops the oldest tail rather than growing
// unbounded (5000-10000+ rows visibly slows the browser).
const maxRetainedObservations = 1000

function History() {
  const [observations, setObservations] = useState<Observation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [cursor, setCursor] = useState<number | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [typeFilter, setTypeFilter] = useState('')

  const fetchPage = useCallback(
    async (reset = false) => {
      setLoading(true)
      setError(null)

      try {
        const res = await apiClient.fetchObservations({
          type: typeFilter || undefined,
          cursor: reset ? undefined : cursor ?? undefined,
          limit: 50,
        })

        setObservations((prev) => {
          const merged = reset ? res.items : [...prev, ...res.items]
          return merged.length > maxRetainedObservations
            ? merged.slice(0, maxRetainedObservations)
            : merged
        })
        setCursor(res.next_cursor)
        setHasMore(res.next_cursor !== null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    },
    [typeFilter, cursor]
  )

  useEffect(() => {
    setCursor(null)
    setObservations([])
    fetchPage(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [typeFilter])

  const loadMore = () => {
    if (!loading && hasMore) {
      fetchPage()
    }
  }

  const filters: { label: string; value: string }[] = [
    { label: 'All', value: '' },
    { label: 'Joins', value: 'player.joined' },
    { label: 'Leaves', value: 'player.left' },
    { label: 'World', value: 'world.joining_observed' },
    { label: 'Media', value: 'resource.url_observed' },
  ]

  return (
    <div className="space-y-4">
      <div className="flex gap-2 flex-wrap">
        {filters.map((f) => (
          <button
            key={f.value}
            onClick={() => setTypeFilter(f.value)}
            className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
              typeFilter === f.value
                ? 'bg-blue-100 text-blue-700'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-red-700">Error: {error}</p>
        </div>
      )}

      <div className="bg-white rounded-lg shadow">
        {observations.length === 0 && !loading ? (
          <div className="p-4 text-center text-gray-500">No observations found</div>
        ) : (
          <ul className="divide-y divide-gray-100">
            {observations.map((obs) => (
              <ObservationRow key={obs.id} obs={obs} />
            ))}
          </ul>
        )}

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

      {loading && observations.length === 0 && (
        <div className="flex items-center justify-center py-8">
          <div className="text-gray-500">Loading...</div>
        </div>
      )}
    </div>
  )
}

export default History
