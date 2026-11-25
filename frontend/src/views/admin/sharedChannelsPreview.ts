import type { AdminSharedChannel } from '@/api/admin/sharedChannels'
import type { AdminGroup } from '@/types'

export const groups: Pick<AdminGroup, 'id' | 'name' | 'platform' | 'status' | 'is_exclusive' | 'subscription_type'>[] = [
  { id: 1, name: 'Gemini 专属分组', platform: 'gemini', status: 'active', is_exclusive: true, subscription_type: 'standard' },
  { id: 2, name: 'Claude 专属分组', platform: 'anthropic', status: 'active', is_exclusive: true, subscription_type: 'standard' },
  { id: 3, name: 'OpenAI 专属分组', platform: 'openai', status: 'active', is_exclusive: true, subscription_type: 'standard' },
]

export const channels: AdminSharedChannel[] = [
  { id: 1, name: 'Gemini Flash 共享服务', group_id: 1, group_name: groups[0]!.name, platform: 'gemini',
    group_eligible: true, max_concurrency: 8, expires_at: null, published: true,
    budget_limit: 100, budget_multiplier: 2, budget_used: 24.68, budget_pricing_blocked: false },
  { id: 2, name: 'Claude 共享服务', group_id: 2, group_name: groups[1]!.name, platform: 'anthropic',
    group_eligible: true, max_concurrency: 5, expires_at: new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString(), published: true,
    budget_limit: null, budget_multiplier: 1, budget_used: 0, budget_pricing_blocked: false },
  { id: 3, name: 'OpenAI 共享服务', group_id: 3, group_name: groups[2]!.name, platform: 'openai',
    group_eligible: true, max_concurrency: 8, expires_at: null, published: false,
    budget_limit: 100, budget_multiplier: 1, budget_used: 100, budget_pricing_blocked: false },
]
