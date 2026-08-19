import { useState, useEffect, useCallback } from 'react'
import { apiClient } from '../api/client'
import type { MediaAttempt } from '../api/types'
import { SafeLink } from '../components/SafeLink'
import { CopyButton } from '../components/CopyButton'

function statusLabel(status: 'observed' | 'failed'): string {
  return status === 'failed' ? '再生失敗' : 'URL検出'
}

function statusBadgeClass(status: 'observed' | 'failed'): string {
  return status === 'failed' ? 'bg-red-100 text-red-800' : 'bg-blue-100 text-blue-800'
}

function AttemptCard({ attempt }: { attempt: MediaAttempt }) {
  return (
    <div className="bg-white rounded-lg shadow p-4 space-y-3">
      <div className="flex items-center justify-between">
        <span className={`px-2 py-0.5 rounded text-xs font-medium ${statusBadgeClass(attempt.status)}`}>
          {statusLabel(attempt.status)}
        </span>
        <span className="text-xs text-gray-400">
          {new Date(attempt.first_observed_at).toLocaleString()} 〜{' '}
          {new Date(attempt.last_observed_at).toLocaleTimeString()}
        </span>
      </div>

      {attempt.best_openable_url && (
        <div>
          <SafeLink url={attempt.best_openable_url} className="text-sm text-blue-600 hover:underline break-all block mb-2" />
          <div className="flex gap-2">
            <CopyButton text={attempt.best_openable_url} />
            <SafeLink
              url={attempt.best_openable_url}
              className="px-3 py-1.5 text-sm rounded-md bg-blue-50 text-blue-700 hover:bg-blue-100 transition-colors"
            >
              ブラウザで開く
            </SafeLink>
          </div>
        </div>
      )}

      {attempt.target && (attempt.target.component || attempt.target.key) && (
        <p className="text-xs text-gray-500">
          {[attempt.target.component, attempt.target.key, attempt.target.backend].filter(Boolean).join(' / ')}
        </p>
      )}

      {attempt.adapter_ids.length > 0 && (
        <p className="text-xs text-gray-500">Adapters: {attempt.adapter_ids.join(', ')}</p>
      )}

      {attempt.resources.length > 0 && (
        <details className="text-sm">
          <summary className="cursor-pointer text-gray-600">Resources ({attempt.resources.length})</summary>
          <ul className="mt-2 space-y-1 pl-2">
            {attempt.resources.map((r, i) => (
              <li key={i} className="text-xs text-gray-600 break-all">
                <span className="font-medium">{r.role}</span>: {r.url}
              </li>
            ))}
          </ul>
        </details>
      )}

      {attempt.errors.length > 0 && (
        <details className="text-sm" open>
          <summary className="cursor-pointer text-gray-600">Errors ({attempt.errors.length})</summary>
          <ul className="mt-2 space-y-1 pl-2">
            {attempt.errors.map((e, i) => (
              <li key={i} className="text-xs text-red-600 break-all">
                [{e.stage}] {e.code} {e.message}
              </li>
            ))}
          </ul>
        </details>
      )}

      <details className="text-xs text-gray-400">
        <summary className="cursor-pointer">Observation IDs</summary>
        <p className="mt-1 break-all">{attempt.observation_ids.join(', ')}</p>
      </details>
    </div>
  )
}

function Media() {
  const [attempts, setAttempts] = useState<MediaAttempt[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    apiClient
      .fetchMediaRecent(50)
      .then((res) => setAttempts(res.attempts))
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load media'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  if (loading && attempts.length === 0) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500">Loading...</div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-gray-800">Media</h1>
        <button onClick={load} className="text-sm text-blue-600 hover:text-blue-800">
          Refresh
        </button>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-red-700">Error: {error}</p>
        </div>
      )}

      {attempts.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-4 text-center text-gray-500">No media attempts yet</div>
      ) : (
        <div className="space-y-3">
          {attempts.map((a) => (
            <AttemptCard key={a.id} attempt={a} />
          ))}
        </div>
      )}
    </div>
  )
}

export default Media
