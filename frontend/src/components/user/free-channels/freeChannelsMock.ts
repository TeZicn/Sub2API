export type HourStatus = 'success' | 'degraded' | 'failure' | 'idle'
export type FreeChannelAvailability = 'open' | 'joined' | 'full'

export interface RequestHealthStatus {
  status: HourStatus
  requests: number
  failures: number
  rateLimits: number
}

export interface HourlyRequestStatus extends RequestHealthStatus {
  hour: number
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
  usernames: string[]
  currentUserJoined: boolean
  recentStatus: RequestHealthStatus
  hourlyStatuses: HourlyRequestStatus[]
}

const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS

function makeHourlyStatuses(
  traffic: Record<number, { requests: number; failures?: number; rateLimits?: number }>,
): HourlyRequestStatus[] {
  return Array.from({ length: 24 }, (_, hour) => {
    const sample = traffic[hour]
    const requests = sample?.requests ?? 0
    const failures = sample?.failures ?? 0
    const rateLimits = sample?.rateLimits ?? 0
    return {
      hour,
      requests,
      failures,
      rateLimits,
      status: healthStatus(requests, failures, rateLimits),
    }
  })
}

function healthStatus(requests: number, failures: number, rateLimits: number): HourStatus {
  if (requests === 0) return 'idle'
  if (failures >= 5 && failures * 2 >= requests) return 'failure'
  if (failures > 0 || rateLimits > 0) return 'degraded'
  return 'success'
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
    recentStatus: { status: 'success', requests: 12, failures: 0, rateLimits: 0 },
    hourlyStatuses: makeHourlyStatuses({
      1: { requests: 12 },
      2: { requests: 18 },
      4: { requests: 23 },
      6: { requests: 19 },
      7: { requests: 31, rateLimits: 2 },
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
    recentStatus: { status: 'degraded', requests: 14, failures: 1, rateLimits: 4 },
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
    recentStatus: { status: 'idle', requests: 0, failures: 0, rateLimits: 0 },
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
    recentStatus: { status: 'failure', requests: 10, failures: 6, rateLimits: 1 },
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
