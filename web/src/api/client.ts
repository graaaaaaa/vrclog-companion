// API client with Basic Auth support.

import type {
  ObservationsResponse,
  StateSnapshot,
  MediaRecentResponse,
  AdaptersResponse,
  ConfigResponse,
  ConfigUpdateRequest,
  ConfigUpdateResponse,
  TokenResponse,
  StatsResponse,
} from './types'

export type {
  Observation,
  ObservationsResponse,
  StateSnapshot,
  CurrentWorld,
  PlayerInfo,
  LatestOpenableMedia,
  MediaAttempt,
  MediaResource,
  MediaError,
  MediaTarget,
  MediaRecentResponse,
  LoadedAdapter,
  AdaptersResponse,
  Health,
  ConfigResponse,
  ConfigUpdateRequest,
  ConfigUpdateResponse,
  TokenResponse,
  StatsResponse,
} from './types'

class ApiClient {
  private credentials: { username: string; password: string } | null = null

  setCredentials(username: string, password: string) {
    this.credentials = { username, password }
  }

  clearCredentials() {
    this.credentials = null
  }

  private getAuthHeader(): HeadersInit {
    if (!this.credentials) {
      return {}
    }
    const encoded = btoa(`${this.credentials.username}:${this.credentials.password}`)
    return { Authorization: `Basic ${encoded}` }
  }

  async fetchState(): Promise<StateSnapshot> {
    const res = await fetch('/api/v1/state', {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch: ${res.status}`)
    }
    return res.json()
  }

  async fetchObservations(params?: {
    type?: string
    adapter_id?: string
    since?: string
    until?: string
    limit?: number
    cursor?: number
  }): Promise<ObservationsResponse> {
    const searchParams = new URLSearchParams()
    if (params?.type) searchParams.set('type', params.type)
    if (params?.adapter_id) searchParams.set('adapter_id', params.adapter_id)
    if (params?.since) searchParams.set('since', params.since)
    if (params?.until) searchParams.set('until', params.until)
    if (params?.limit) searchParams.set('limit', params.limit.toString())
    if (params?.cursor !== undefined) searchParams.set('cursor', params.cursor.toString())

    const url = `/api/v1/observations${searchParams.toString() ? '?' + searchParams.toString() : ''}`
    const res = await fetch(url, {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch: ${res.status}`)
    }
    return res.json()
  }

  async fetchMediaRecent(limit?: number): Promise<MediaRecentResponse> {
    const url = `/api/v1/media/recent${limit ? `?limit=${limit}` : ''}`
    const res = await fetch(url, {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch: ${res.status}`)
    }
    return res.json()
  }

  async fetchAdapters(): Promise<AdaptersResponse> {
    const res = await fetch('/api/v1/adapters', {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch: ${res.status}`)
    }
    return res.json()
  }

  async fetchConfig(): Promise<ConfigResponse> {
    const res = await fetch('/api/v1/config', {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch: ${res.status}`)
    }
    return res.json()
  }

  async updateConfig(req: ConfigUpdateRequest): Promise<ConfigUpdateResponse> {
    const res = await fetch('/api/v1/config', {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        ...this.getAuthHeader(),
      },
      body: JSON.stringify(req),
    })
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: 'Unknown error' }))
      throw new Error(error.error || `Failed to update: ${res.status}`)
    }
    return res.json()
  }

  async fetchToken(): Promise<TokenResponse> {
    const res = await fetch('/api/v1/auth/token', {
      method: 'POST',
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch token: ${res.status}`)
    }
    return res.json()
  }

  async fetchStats(): Promise<StatsResponse> {
    const res = await fetch('/api/v1/stats/basic', {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch stats: ${res.status}`)
    }
    return res.json()
  }
}

export const apiClient = new ApiClient()
