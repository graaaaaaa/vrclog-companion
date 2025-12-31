// API client with Basic Auth support

export interface WorldInfo {
  WorldID: string
  WorldName: string
  InstanceID: string
  JoinedAt: string
}

export interface PlayerInfo {
  PlayerName: string
  PlayerID: string
  JoinedAt: string
}

export interface NowResponse {
  world: WorldInfo | null
  players: PlayerInfo[]
}

export interface Event {
  id: number
  type: string
  ts: string
  world_id?: string
  world_name?: string
  instance_id?: string
  player_name?: string
  player_id?: string
}

export interface EventsResponse {
  items: Event[]
  next_cursor: string | null
}

export interface ConfigResponse {
  port: number
  lan_enabled: boolean
  discord_batch_sec: number
  notify_on_join: boolean
  notify_on_leave: boolean
  notify_on_world_join: boolean
  discord_webhook_configured: boolean
}

export interface ConfigUpdateRequest {
  port?: number
  lan_enabled?: boolean
  discord_batch_sec?: number
  notify_on_join?: boolean
  notify_on_leave?: boolean
  notify_on_world_join?: boolean
  discord_webhook_url?: string
}

export interface ConfigUpdateResponse {
  success: boolean
  restart_required: boolean
  new_port?: number
}

export interface TokenResponse {
  token: string
  expires_in: number
}

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

  async fetchNow(): Promise<NowResponse> {
    const res = await fetch('/api/v1/now', {
      headers: this.getAuthHeader(),
    })
    if (!res.ok) {
      throw new Error(`Failed to fetch: ${res.status}`)
    }
    return res.json()
  }

  async fetchEvents(params?: {
    type?: string
    since?: string
    until?: string
    limit?: number
    cursor?: string
  }): Promise<EventsResponse> {
    const searchParams = new URLSearchParams()
    if (params?.type) searchParams.set('type', params.type)
    if (params?.since) searchParams.set('since', params.since)
    if (params?.until) searchParams.set('until', params.until)
    if (params?.limit) searchParams.set('limit', params.limit.toString())
    if (params?.cursor) searchParams.set('cursor', params.cursor)

    const url = `/api/v1/events${searchParams.toString() ? '?' + searchParams.toString() : ''}`
    const res = await fetch(url, {
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
}

export const apiClient = new ApiClient()
