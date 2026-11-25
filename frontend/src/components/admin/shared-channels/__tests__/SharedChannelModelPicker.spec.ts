import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import * as accountsAPI from '@/api/admin/accounts'
import type { AccountListItem } from '@/types'
import SharedChannelModelPicker from '../SharedChannelModelPicker.vue'

vi.mock('@/api/admin/accounts', () => ({ list: vi.fn(), syncUpstreamModels: vi.fn() }))
enableAutoUnmount(afterEach)
const account = (id: number) => ({ id, name: `Account ${id}`, platform: 'openai' }) as AccountListItem
const page = (ids: number[], pages = 1) => ({ items: ids.map(account), page: 1, page_size: 100, total: ids.length, pages })

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(accountsAPI.list).mockResolvedValue(page([11, 12]))
  vi.mocked(accountsAPI.syncUpstreamModels).mockResolvedValue({ models: [' model-a ', 'model-b', 'model-a'] })
})

function render(props: { groupId?: number; modelValue?: string[]; previewModels?: string[] } = {}) {
  const wrapper = mount(SharedChannelModelPicker, {
    props: { groupId: 1, modelValue: [], ...props },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] },
  })
  wrapper.setProps({ 'onUpdate:modelValue': (models: string[]) => { void wrapper.setProps({ modelValue: models }) } })
  return wrapper
}

describe('SharedChannelModelPicker', () => {
  it('waits for a group and keeps preview synchronization local', async () => {
    const wrapper = render({ groupId: 0, previewModels: ['preview-model'] })
    await flushPromises()
    expect(accountsAPI.list).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Choose a linked group')
    await wrapper.setProps({ groupId: 1 })
    await flushPromises()
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    expect(wrapper.text()).toContain('preview-model')
    expect(accountsAPI.syncUpstreamModels).not.toHaveBeenCalled()
  })

  it('syncs through the account API and requires explicit selection before saving', async () => {
    const wrapper = render({ modelValue: ['existing-model'] })
    await flushPromises()
    expect(accountsAPI.list).toHaveBeenCalledWith(1, 100, { group: '1', status: 'active', lite: 'true' })
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await flushPromises()
    expect(accountsAPI.syncUpstreamModels).toHaveBeenCalledWith(11)
    expect(wrapper.props('modelValue')).toEqual(['existing-model'])
    expect(wrapper.findAll('[data-testid="shared-model-option"]')).toHaveLength(3)
    await wrapper.get('input[value="model-a"]').setValue(true)
    expect(wrapper.props('modelValue')).toEqual(['existing-model', 'model-a'])
    await wrapper.get('[data-testid="shared-model-search"]').setValue('model-b')
    expect(wrapper.findAll('[data-testid="shared-model-option"]')).toHaveLength(1)
    await wrapper.findAll('button').find(button => button.text() === 'Select search results')!.trigger('click')
    expect(wrapper.props('modelValue')).toEqual(['existing-model', 'model-a', 'model-b'])
  })

  it('loads every page of eligible accounts', async () => {
    vi.mocked(accountsAPI.list).mockResolvedValueOnce(page([11], 2)).mockResolvedValueOnce(page([12], 2))
    const wrapper = render()
    await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(2, 100, expect.objectContaining({ group: '1' }))
    expect(wrapper.findAll('select option')).toHaveLength(2)
  })

  it('ignores an old sync after switching source accounts', async () => {
    let finish!: (result: accountsAPI.SyncUpstreamModelsResult) => void
    vi.mocked(accountsAPI.syncUpstreamModels).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = render({ modelValue: ['saved'] })
    await flushPromises()
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await wrapper.get('select').setValue('12')
    finish({ models: ['stale-model'] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale-model')
    expect(wrapper.props('modelValue')).toEqual(['saved'])
    expect(wrapper.get<HTMLButtonElement>('[data-testid="sync-shared-models"]').element.disabled).toBe(false)
  })

  it('ignores old account lists and sync results after changing groups', async () => {
    let finishList!: (value: ReturnType<typeof page>) => void
    vi.mocked(accountsAPI.list).mockImplementationOnce(() => new Promise(resolve => { finishList = resolve }))
    const wrapper = render({ modelValue: ['saved'] })
    await wrapper.setProps({ groupId: 2 })
    await flushPromises()
    finishList(page([99]))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Account 99')
    let finishSync!: (result: accountsAPI.SyncUpstreamModelsResult) => void
    vi.mocked(accountsAPI.syncUpstreamModels).mockImplementationOnce(() => new Promise(resolve => { finishSync = resolve }))
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await wrapper.setProps({ groupId: 3 })
    await flushPromises()
    finishSync({ models: ['old-group-model'] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('old-group-model')
    expect(wrapper.props('modelValue')).toEqual(['saved'])
  })

  it('reports sync failure, empty results, and configured fallback without replacing selections', async () => {
    const wrapper = render({ modelValue: ['saved'] })
    await flushPromises()
    vi.mocked(accountsAPI.syncUpstreamModels).mockRejectedValueOnce({ message: 'Upstream unavailable' })
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Upstream unavailable')
    vi.mocked(accountsAPI.syncUpstreamModels).mockResolvedValueOnce({ models: [] })
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    vi.mocked(accountsAPI.syncUpstreamModels).mockResolvedValueOnce({ models: ['fallback-model'], discovery_source: 'configured_fallback' })
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('fallback-model')
    expect(wrapper.props('modelValue')).toEqual(['saved'])
  })

  it('does not silently truncate selection beyond the 100 model limit', async () => {
    vi.mocked(accountsAPI.syncUpstreamModels).mockResolvedValue({ models: Array.from({ length: 101 }, (_, i) => `model-${i}`) })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="sync-shared-models"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Select search results')!.trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toContain('100 models')
    expect(wrapper.props('modelValue')).toEqual([])
    await wrapper.get('[data-testid="shared-model-search"]').setValue('model-100')
    await wrapper.findAll('button').find(button => button.text() === 'Select search results')!.trigger('click')
    expect(wrapper.props('modelValue')).toEqual(['model-100'])
  })

  it('shows account load errors and offers a retry', async () => {
    vi.mocked(accountsAPI.list).mockRejectedValueOnce({ message: 'Unable to list accounts' })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Unable to list accounts')
    await wrapper.findAll('button').find(button => button.text() === 'Refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('select').element.value).toBe('11')
  })
})
