import { apiClient } from '../client'

export interface AdminSharedChannel {
  id: number
  name: string
  models: string[]
  group_id: number
  group_name: string
  platform: string
  group_eligible: boolean
  max_concurrency: number
  expires_at: string | null
  published: boolean
  budget_limit: number | null
  budget_multiplier: number
  budget_used: number
  budget_pricing_blocked: boolean
}

export interface SharedChannelInput {
  name: string
  models: string[]
  group_id: number
  max_concurrency: number
  expires_at: string | null
  published: boolean
  budget_limit: number | null
  budget_multiplier: number
}

export async function list(): Promise<AdminSharedChannel[]> {
  const { data } = await apiClient.get<AdminSharedChannel[]>('/admin/shared-channels')
  return data
}

export async function save(id: number | null, input: SharedChannelInput): Promise<AdminSharedChannel> {
  const { data } = id === null
    ? await apiClient.post<AdminSharedChannel>('/admin/shared-channels', input)
    : await apiClient.put<AdminSharedChannel>(`/admin/shared-channels/${id}`, input)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/shared-channels/${id}`)
}
