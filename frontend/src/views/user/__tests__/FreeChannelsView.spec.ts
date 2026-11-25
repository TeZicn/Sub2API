import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

import en from '@/i18n/locales/en'
import { estimateConcurrencyShare } from '@/components/user/free-channels/freeChannelsMock'
import FreeChannelsView from '../FreeChannelsView.vue'
import * as sharedChannelsAPI from '@/api/sharedChannels'
import type { SharedChannel } from '@/api/sharedChannels'

vi.mock('@/api/sharedChannels', () => ({ list: vi.fn(), createKey: vi.fn() }))
enableAutoUnmount(afterEach)

describe('free channel concurrency share', () => {
  it('splits odd capacity as a range after a new user joins', () => {
    expect(estimateConcurrencyShare(5, 1)).toEqual({ min: 2, max: 3 })
  })

  it('keeps one slot for a user joining a nearly full pool', () => {
    expect(estimateConcurrencyShare(5, 4)).toEqual({ min: 1, max: 1 })
  })

  it('rejects a join when all concurrency slots already have users', () => {
    expect(estimateConcurrencyShare(5, 5)).toBeNull()
  })

  it('caps shared requests by the existing personal limit', () => {
    expect(estimateConcurrencyShare(8, 0, 5)).toEqual({ min: 5, max: 5 })
    expect(estimateConcurrencyShare(8, 1, 5)).toEqual({ min: 4, max: 4 })
    expect(estimateConcurrencyShare(8, 2, 5)).toEqual({ min: 2, max: 3 })
    expect(estimateConcurrencyShare(8, 7, 5)).toEqual({ min: 1, max: 1 })
  })
})

describe('FreeChannelsView', () => {
  function mountView(preview = true) {
    window.history.replaceState({}, '', preview ? '/free-channels?preview=1' : '/free-channels')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
    return mount(FreeChannelsView, {
      global: {
        plugins: [i18n],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        },
      },
    })
  }

  it('renders a 24-hour request history for each sample channel', async () => {
    const wrapper = mountView()
    await flushPromises()
    const cards = wrapper.findAll('[data-testid="free-channel-card"]')

    expect(cards.length).toBeGreaterThan(0)
    for (const card of cards) {
      expect(card.findAll('[data-testid="hour-status-cell"]')).toHaveLength(24)
    }
  })

  it('filters channels by name', async () => {
    const wrapper = mountView()
    const search = wrapper.find('[data-testid="free-channel-search"]')

    await search.setValue('Gemini')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="free-channel-card"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Gemini')
  })

  it('opens the local preview join dialog without sending a real key request', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-testid="join-free-channel-gemini-pool"]').trigger('click')

    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('design preview')
    const dialog = wrapper.find('[data-testid="join-free-channel-dialog"]')
    expect(dialog.text()).not.toContain('Estimated share after joining')
    expect(dialog.text()).not.toContain('All keys created by one account')
    expect(dialog.find('input').exists()).toBe(true)
  })

  it('reveals a user name only when their avatar is clicked', async () => {
    const wrapper = mountView()
    await flushPromises()
    const card = wrapper.find('[data-testid="free-channel-card"]')
    expect(card.text()).not.toContain('cloud_walker')
    const avatar = card.find('[data-testid="shared-user-avatar"]')
    await avatar.trigger('click')
    expect(card.find('[role="tooltip"]').text()).toBe('cloud_walker')
    expect(avatar.attributes('aria-expanded')).toBe('true')
    await avatar.trigger('click')
    expect(card.find('[role="tooltip"]').exists()).toBe(false)
  })

  it('allows creating a key while the active request pool is busy', async () => {
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([publishedChannel({
      max_concurrency: 1, usernames: ['active-user'], share: 0,
    })])
    const wrapper = mountView(false)
    await flushPromises()
    const join = wrapper.find('[data-testid="join-free-channel-1"]')
    expect(join.attributes('disabled')).toBeUndefined()
    await join.trigger('click')
    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(true)
  })

  it('shows open models and expands the remaining model names', async () => {
    const wrapper = mountView()
    await flushPromises()
    const card = wrapper.find('[data-testid="free-channel-card"]')
    expect(card.findAll('[data-testid="shared-channel-model"]')).toHaveLength(3)
    const expand = card.find('button[aria-expanded]')
    expect(expand.attributes('aria-expanded')).toBe('false')
    await expand.trigger('click')
    expect(card.findAll('[data-testid="shared-channel-model"]')).toHaveLength(4)
    expect(expand.attributes('aria-expanded')).toBe('true')
    await expand.trigger('click')
    expect(card.findAll('[data-testid="shared-channel-model"]')).toHaveLength(3)
  })

  it('finds services by an open model and displays budget usage', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-testid="free-channel-search"]').setValue('deepseek-reasoner')
    expect(wrapper.findAll('[data-testid="free-channel-card"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('deepseek-reasoner')
    expect(wrapper.find('[role="progressbar"]').attributes('aria-valuenow')).toBe('0')
  })

  it('shows only the selected end mode and omits the share text row', async () => {
    const wrapper = mountView()
    await flushPromises()
    const cards = wrapper.findAll('[data-testid="free-channel-card"]')
    const budgetCard = cards.find(card => card.text().includes('Gemini Flash'))!
    const timeCard = cards.find(card => card.text().includes('Claude 共享服务'))!
    expect(budgetCard.text()).toContain('Remaining budget')
    expect(budgetCard.text()).toContain('$75.32')
    expect(budgetCard.text()).not.toContain('LISTING EXPIRES IN')
    expect(timeCard.text()).toContain('LISTING EXPIRES IN')
    expect(timeCard.find('[role="progressbar"]').exists()).toBe(false)
    for (const card of cards) {
      expect(card.text()).not.toContain('Requests per user after joining')
      expect(card.text()).not.toContain('per user')
    }
  })

  function publishedChannel(overrides: Partial<SharedChannel> = {}): SharedChannel {
    return {
      id: 1, name: 'Live service', platform: 'openai', models: ['gpt-5'], max_concurrency: 8,
      expires_at: null, budget_limit: 100, budget_used: 20, usernames: [], joined: false, share: 5, hours: [],
      ...overrides,
    }
  }

  it('removes an expired card and its pending join dialog while the page stays open', async () => {
    vi.useFakeTimers()
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([
      publishedChannel({ budget_limit: null, expires_at: new Date(Date.now() + 1000).toISOString() }),
    ])
    const wrapper = mountView(false)
    await flushPromises()
    const hours = wrapper.findAll('[data-testid="hour-status-cell"]')
    expect(hours[0]!.attributes('title')).toContain('23–24h ago')
    expect(hours[23]!.attributes('title')).toContain('0–1h ago')
    await wrapper.find('[data-testid="join-free-channel-1"]').trigger('click')
    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(1500)
    expect(wrapper.findAll('[data-testid="free-channel-card"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(false)
  })

  it('removes exhausted budgets and their pending join dialog on refresh', async () => {
    vi.useFakeTimers()
    vi.mocked(sharedChannelsAPI.list)
      .mockResolvedValueOnce([publishedChannel()])
      .mockResolvedValueOnce([publishedChannel({ budget_used: 100 })])
    const wrapper = mountView(false)
    await flushPromises()
    await wrapper.find('[data-testid="join-free-channel-1"]').trigger('click')
    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(15_000)
    expect(wrapper.findAll('[data-testid="free-channel-card"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(false)
  })

  it('defaults the key name to the service name and displays and copies the created key', async () => {
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([publishedChannel()])
    vi.mocked(sharedChannelsAPI.createKey).mockResolvedValue({
      id: 10, key: 'preview-test-key', name: 'Live service', group_id: 2, status: 'active', expires_at: null,
    })
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mountView(false)
    await flushPromises()
    await wrapper.find('[data-testid="join-free-channel-1"]').trigger('click')
    const dialog = wrapper.find('[data-testid="join-free-channel-dialog"]')
    expect((dialog.find('input').element as HTMLInputElement).value).toBe('Live service')
    await wrapper.findAll('button').find(button => button.text() === 'Join and create API key')!.trigger('click')
    await flushPromises()
    expect(sharedChannelsAPI.createKey).toHaveBeenCalledWith(1, 'Live service')
    expect(dialog.find('code').text()).toBe('preview-test-key')
    await dialog.find('button').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('preview-test-key')
    expect(dialog.text()).toContain('Copied')
  })

  it('keeps the dialog open while creating a key so the result is not lost', async () => {
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([publishedChannel()])
    let finish!: (value: Awaited<ReturnType<typeof sharedChannelsAPI.createKey>>) => void
    vi.mocked(sharedChannelsAPI.createKey).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountView(false)
    await flushPromises()
    await wrapper.find('[data-testid="join-free-channel-1"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'Join and create API key')!.trigger('click')
    const cancel = wrapper.findAll('button').find(button => button.text() === 'Cancel')!
    expect(cancel.attributes('disabled')).toBeDefined()
    await cancel.trigger('click')
    expect(wrapper.find('[data-testid="join-free-channel-dialog"]').exists()).toBe(true)
    finish({ id: 10, key: 'created-test-key', name: 'Live service', group_id: 2, status: 'active', expires_at: null })
    await flushPromises()
    expect(wrapper.find('code').text()).toBe('created-test-key')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.resetAllMocks()
    vi.unstubAllGlobals()
    window.history.replaceState({}, '', '/')
  })
})
