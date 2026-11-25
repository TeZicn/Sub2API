export type HourStatus = 'success' | 'failure' | 'idle'
export type FreeChannelAvailability = 'open' | 'joined' | 'full'

export interface HourlyRequestStatus {
  hour: number
  status: HourStatus
  requests: number
  failures: number
}

export interface FreeChannelMock {
  id: string
  name: string
  provider: string
  models: string[]
  availability: FreeChannelAvailability
  expiresAt: string | null
  budgetLimit?: number | null
  budgetUsed?: number
  maxConcurrency: number
  personalConcurrency?: number
  usernames: string[]
  currentUserJoined: boolean
  hourlyStatuses: HourlyRequestStatus[]
}

const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS

function makeHourlyStatuses(
  traffic: Record<number, { requests: number; failures?: number }>,
): HourlyRequestStatus[] {
  return Array.from({ length: 24 }, (_, hour) => {
    const sample = traffic[hour]
    const requests = sample?.requests ?? 0
    const failures = sample?.failures ?? 0
    return {
      hour,
      requests,
      failures,
      status: requests === 0 ? 'idle' : failures > 0 ? 'failure' : 'success',
    }
  })
}

export const freeChannelSamples: FreeChannelMock[] = [
  {
    id: 'gemini-pool',
    name: 'Gemini Flash 共享服务',
    provider: 'gemini',
    models: ['gemini-2.5-flash', 'gemini-2.5-pro', 'gemini-2.5-flash-lite', 'gemini-3-pro-preview'],
    budgetLimit: 100,
    budgetUsed: 24.68,
    availability: 'open',
    expiresAt: null,
    maxConcurrency: 5,
    usernames: ['cloud_walker'],
    currentUserJoined: false,
    hourlyStatuses: makeHourlyStatuses({
      1: { requests: 12 },
      2: { requests: 18 },
      4: { requests: 23 },
      6: { requests: 19 },
      7: { requests: 31 },
      9: { requests: 27 },
      11: { requests: 14 },
      13: { requests: 36 },
      14: { requests: 28 },
      16: { requests: 42 },
      18: { requests: 38 },
      20: { requests: 51 },
      21: { requests: 46 },
      22: { requests: 33 },
    }),
  },
  {
    id: 'claude-community',
    name: 'Claude 共享服务',
    provider: 'anthropic',
    models: ['claude-sonnet-4-5', 'claude-haiku-4-5'],
    availability: 'open',
    expiresAt: new Date(Date.now() + 3 * DAY_MS + 7 * HOUR_MS).toISOString(),
    maxConcurrency: 8,
    usernames: ['river_lin', 'mori', '小满'],
    currentUserJoined: false,
    hourlyStatuses: makeHourlyStatuses({
      0: { requests: 8 },
      2: { requests: 12 },
      3: { requests: 9, failures: 1 },
      5: { requests: 18 },
      7: { requests: 22 },
      8: { requests: 19, failures: 2 },
      10: { requests: 25 },
      12: { requests: 31 },
      13: { requests: 28 },
      15: { requests: 36, failures: 1 },
      17: { requests: 44 },
      18: { requests: 39 },
      19: { requests: 52 },
      21: { requests: 47 },
      23: { requests: 33 },
    }),
  },
  {
    id: 'deepseek-lab',
    name: 'DeepSeek 共享服务',
    provider: 'deepseek',
    models: ['deepseek-chat', 'deepseek-reasoner'],
    budgetLimit: 100,
    budgetUsed: 0,
    availability: 'open',
    expiresAt: null,
    maxConcurrency: 3,
    usernames: [],
    currentUserJoined: false,
    hourlyStatuses: makeHourlyStatuses({}),
  },
  {
    id: 'openai-after-hours',
    name: 'OpenAI 共享服务',
    provider: 'openai',
    models: ['gpt-5', 'gpt-5-mini', 'gpt-5-nano', 'gpt-4.1'],
    availability: 'full',
    expiresAt: new Date(Date.now() + 7 * HOUR_MS).toISOString(),
    maxConcurrency: 4,
    usernames: ['nora', 'echo_7', '小北', 'atlas'],
    currentUserJoined: false,
    hourlyStatuses: makeHourlyStatuses({
      1: { requests: 6 },
      4: { requests: 11 },
      6: { requests: 12, failures: 3 },
      8: { requests: 15 },
      10: { requests: 19 },
      12: { requests: 22, failures: 2 },
      14: { requests: 29 },
      16: { requests: 31 },
      18: { requests: 37 },
      20: { requests: 40, failures: 1 },
      22: { requests: 33 },
    }),
  },
]

export function estimateConcurrencyShare(
  maxConcurrency: number,
  currentParticipants: number,
  personalConcurrency?: number,
): { min: number; max: number } | null {
  if (!Number.isInteger(maxConcurrency) || maxConcurrency <= 0) return null
  if (!Number.isInteger(currentParticipants) || currentParticipants < 0) return null

  const participantsAfterJoin = currentParticipants + 1
  if (participantsAfterJoin > maxConcurrency) return null

  const average = maxConcurrency / participantsAfterJoin
  const cap = personalConcurrency != null && personalConcurrency > 0 ? personalConcurrency : maxConcurrency
  return { min: Math.min(cap, Math.floor(average)), max: Math.min(cap, Math.ceil(average)) }
}
