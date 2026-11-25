import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { useAppStore } from '@/stores/app'

import en from '@/i18n/locales/en'
import { i18n as appI18n } from '@/i18n'
import FreeChannelsView from '../FreeChannelsView.vue'
import * as sharedChannelsAPI from '@/api/sharedChannels'
import type { SharedChannel } from '@/api/sharedChannels'

vi.mock('@/api/sharedChannels', () => ({ list: vi.fn(), createKey: vi.fn() }))
appI18n.global.setLocaleMessage('en', en)
enableAutoUnmount(afterEach)

describe('FreeChannelsView', () => {
  function mountView(preview = true, apiBaseUrl = '') {
    window.history.replaceState({}, '', preview ? '/free-channels?preview=1' : '/free-channels')
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
    const pinia = createPinia()
    useAppStore(pinia).apiBaseUrl = apiBaseUrl
    return mount(FreeChannelsView, {
      global: {
        plugins: [i18n, pinia],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: { name: 'BaseDialog', props: ['show', 'showCloseButton', 'closeOnEscape', 'closeOnClickOutside'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        },
      },
    })
  }

  it('renders a 24-hour request history for each sample channel', async () => {
    const wrapper = mountView()
    await vi.dynamicImportSettled()
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

  it('shows email prefixes from the API and opens only the clicked avatar for duplicate names', async () => {
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([publishedChannel({ usernames: ['alice', 'bob', 'same-name', 'same-name'] })])
    const wrapper = mountView(false)
    await flushPromises()
    const avatars = wrapper.findAll('[data-testid="shared-user-avatar"]')
    expect(avatars).toHaveLength(4)
    expect(avatars.every(avatar => avatar.text().length > 0)).toBe(true)
    await avatars[0]!.trigger('click')
    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toBe('alice')
    await avatars[1]!.trigger('click')
    expect(wrapper.get('[role="tooltip"]').text()).toBe('bob')
    await avatars[2]!.trigger('click')
    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toBe('same-name')
    expect(avatars[3]!.attributes('aria-expanded')).toBe('false')
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
    vi.stubGlobal('isSecureContext', true)
    const wrapper = mountView(false, ' https://api.example.com/v1/ ')
    await flushPromises()
    await wrapper.find('[data-testid="join-free-channel-1"]').trigger('click')
    const dialog = wrapper.find('[data-testid="join-free-channel-dialog"]')
    expect((dialog.find('input').element as HTMLInputElement).value).toBe('Live service')
    await wrapper.findAll('button').find(button => button.text() === 'Join and create API key')!.trigger('click')
    await flushPromises()
    expect(sharedChannelsAPI.createKey).toHaveBeenCalledWith(1, 'Live service')
    expect(dialog.get('[data-testid="shared-key-value"]').text()).toBe('preview-test-key')
    await dialog.get('[data-testid="copy-shared-key"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('preview-test-key')
    expect(dialog.text()).toContain('Copied')
    expect(dialog.get('[data-testid="shared-endpoint-value"]').text()).toBe('https://api.example.com/v1/')
    expect(dialog.get('[data-testid="copy-shared-endpoint"]').text()).toBe('Copy')
    await dialog.get('[data-testid="copy-shared-endpoint"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('https://api.example.com/v1/')
    expect(dialog.get('[data-testid="copy-shared-endpoint"]').text()).toBe('Copied')
    expect(dialog.get('[data-testid="copy-shared-key"]').text()).toBe('Copied')
    expect(wrapper.findAll('button').some(button => button.text() === 'Cancel')).toBe(false)
  })

  it('displays and copies the configured endpoint before joining without creating a key', async () => {
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([publishedChannel()])
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    vi.stubGlobal('isSecureContext', true)
    const wrapper = mountView(false, ' https://api.example.com/custom/v1/ ')
    await flushPromises()
    await wrapper.get('[data-testid="join-free-channel-1"]').trigger('click')
    expect(wrapper.get('[data-testid="shared-endpoint-value"]').text()).toBe('https://api.example.com/custom/v1/')
    await wrapper.get('[data-testid="copy-shared-endpoint"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('https://api.example.com/custom/v1/')
    expect(sharedChannelsAPI.createKey).not.toHaveBeenCalled()
  })

  it.each(['', '   \n '])('hides an empty endpoint instead of using the website origin (%j)', async apiBaseUrl => {
    const wrapper = mountView(true, apiBaseUrl)
    await flushPromises()
    await wrapper.get('[data-testid="join-free-channel-gemini-pool"]').trigger('click')
    const dialog = wrapper.get('[data-testid="join-free-channel-dialog"]')
    expect(dialog.find('[data-testid="shared-endpoint-field"]').exists()).toBe(false)
    expect(dialog.text()).not.toContain(window.location.origin)
    useAppStore().apiBaseUrl = 'https://api.example.com'
    await flushPromises()
    expect(dialog.get('[data-testid="shared-endpoint-value"]').text()).toBe('https://api.example.com')
    useAppStore().apiBaseUrl = ''
    await flushPromises()
    expect(dialog.find('[data-testid="shared-endpoint-field"]').exists()).toBe(false)
  })

  it('shows a copy failure without claiming success and allows retry', async () => {
    const writeText = vi.fn().mockRejectedValueOnce(new Error('clipboard denied')).mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    vi.stubGlobal('isSecureContext', true)
    const wrapper = mountView(true, 'https://api.example.com')
    await flushPromises()
    await wrapper.get('[data-testid="join-free-channel-gemini-pool"]').trigger('click')
    const field = wrapper.get('[data-testid="shared-endpoint-field"]')
    await field.get('button').trigger('click')
    await flushPromises()
    expect(field.get('[role="alert"]').text()).toBe(en.common.copyFailed)
    expect(field.get('button').text()).toBe('Copy')
    await field.get('button').trigger('click')
    await flushPromises()
    expect(field.find('[role="alert"]').exists()).toBe(false)
    expect(field.get('button').text()).toBe('Copied')
  })

  it('keeps the dialog open while creating a key so the result is not lost', async () => {
    vi.mocked(sharedChannelsAPI.list).mockResolvedValue([publishedChannel()])
    let finish!: (value: Awaited<ReturnType<typeof sharedChannelsAPI.createKey>>) => void
    vi.mocked(sharedChannelsAPI.createKey).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountView(false)
    await flushPromises()
    await wrapper.find('[data-testid="join-free-channel-1"]').trigger('click')
    const createButton = wrapper.findAll('button').find(button => button.text() === 'Join and create API key')!
    await createButton.trigger('click')
    expect(createButton.attributes('disabled')).toBeDefined()
    const modal = wrapper.findComponent({ name: 'BaseDialog' })
    expect(modal.props('showCloseButton')).toBe(false)
    expect(modal.props('closeOnEscape')).toBe(false)
    expect(modal.props('closeOnClickOutside')).toBe(false)
    modal.vm.$emit('close')
    await flushPromises()
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
