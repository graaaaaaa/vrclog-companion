// TypeScript types mirroring the Observation/Projector API model.

export interface ObservationRecord {
  id: string
  source_id: string
  offset: number
  line: number
}

export interface Observation {
  sequence: number
  id: string
  occurred_at: string
  type: string
  payload: Record<string, unknown>
  adapter_id: string
  rule_id: string
  record: ObservationRecord
  ingested_at: string
}

export interface ObservationsResponse {
  items: Observation[]
  next_cursor: number | null
}

export interface CurrentWorld {
  id: string
  name: string
  instance_id: string
  joined_at: string
}

export interface PlayerInfo {
  id: string
  display_name: string
  joined_at: string
}

export interface LatestOpenableMedia {
  attempt_id: string
  url: string
  status: 'observed' | 'failed'
  observed_at: string
}

export interface StateSnapshot {
  world: CurrentWorld | null
  players: PlayerInfo[]
  latest_openable_media?: LatestOpenableMedia | null
}

export interface MediaResource {
  url: string
  kind: string
  role: string
  adapter_id: string
  rule_id: string
  observation_id: string
  observed_at: string
}

export interface MediaError {
  stage: string
  code?: string
  message?: string
  adapter_id: string
  observation_id: string
  observed_at: string
}

export interface MediaTarget {
  component?: string
  key?: string
  backend?: string
}

export interface MediaAttempt {
  id: string
  first_observed_at: string
  last_observed_at: string
  status: 'observed' | 'failed'
  best_openable_url?: string
  resources: MediaResource[]
  errors: MediaError[]
  observation_ids: string[]
  adapter_ids: string[]
  target?: MediaTarget | null
  world_instance_id?: string
}

export interface MediaRecentResponse {
  attempts: MediaAttempt[]
}

export interface LoadedAdapter {
  id: string
  origin: 'core' | 'community'
}

export interface AdaptersResponse {
  adapters: LoadedAdapter[]
}

export interface Health {
  status: 'ok' | 'degraded'
  database: 'ok' | 'error'
  ingest: string
  last_ingest_error: string
  last_record_at: string
  loaded_adapters: number
}

export interface ConfigResponse {
  port: number
  lan_enabled: boolean
  discord_batch_sec: number
  notify_on_join: boolean
  notify_on_leave: boolean
  notify_on_world_join: boolean
  discord_webhook_configured: boolean
  log_path: string
  basic_auth_username?: string
  basic_auth_configured: boolean
}

export interface ConfigUpdateRequest {
  port?: number
  lan_enabled?: boolean
  discord_batch_sec?: number
  notify_on_join?: boolean
  notify_on_leave?: boolean
  notify_on_world_join?: boolean
  discord_webhook_url?: string
  log_path?: string
  basic_auth_password?: string
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

export interface StatsResponse {
  today_joins: number
  today_leaves: number
  today_world_changes: number
  recent_players: string[]
  last_observation_at: string | null
  observations_by_type: Record<string, number>
  observations_by_adapter: Record<string, number>
  media_attempts: number
  media_failures: number
}
