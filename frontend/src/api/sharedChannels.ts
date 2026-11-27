import { apiClient } from './client'

export type SharedChannelHealthStatus = 'idle' | 'success' | 'degraded' | 'failure'

export interface SharedChannelHealth {
  requests: number
  failures: number
  rate_limits: number
  status: SharedChannelHealthStatus
}

export interface SharedChannelHour {
  started_at: string
  requests: number
  failures: number
  rate_limits: number
  status: SharedChannelHealthStatus
}

export interface SharedChannel {
  id: number
  name: string
  platform: string
  models: string[]
  max_concurrency: number
  expires_at: string | null
  budget_limit: number | null
  budget_used: number
  usernames: string[]
  joined: boolean
  share: number
  recent: SharedChannelHealth
  hours: SharedChannelHour[]
}

export interface CreatedSharedChannelKey {
  id: number
  key: string
  name: string
  group_id: number | null
  status: string
  expires_at: string | null
}

export async function list(): Promise<SharedChannel[]> {
  const { data } = await apiClient.get<SharedChannel[]>('/shared-channels')
  return data
}

export async function createKey(channelID: number, name: string): Promise<CreatedSharedChannelKey> {
  const { data } = await apiClient.post<CreatedSharedChannelKey>(`/shared-channels/${channelID}/keys`, { name })
  return data
}
