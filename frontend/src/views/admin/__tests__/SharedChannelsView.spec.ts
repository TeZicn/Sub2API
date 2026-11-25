import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import SharedChannelsView from '../SharedChannelsView.vue'
import * as sharedChannelAPI from '@/api/admin/sharedChannels'
import SharedChannelModelPicker from '@/components/admin/shared-channels/SharedChannelModelPicker.vue'

vi.mock('@/api/admin/sharedChannels', () => ({ list: vi.fn(async () => []), save: vi.fn(async () => ({})), remove: vi.fn(async () => {}) }))
vi.mock('@/api/admin/accounts', () => {
  const api = { list: vi.fn(async () => ({ items: [], pages: 1 })), syncUpstreamModels: vi.fn() }
  return { ...api, default: api }
})
vi.mock('@/api/admin/groups', () => {
  const getAll = vi.fn(async () => [
    { id: 1, name: 'Exclusive group', platform: 'openai', status: 'active', is_exclusive: true, subscription_type: 'standard' },
  ])
  return { getAll, default: { getAll } }
})
enableAutoUnmount(afterEach)
afterEach(() => vi.clearAllMocks())

describe('SharedChannelsView end modes', () => {
  it('keeps preview reads and edits local without calling admin APIs', async () => {
    const wrapper = mount(SharedChannelsView, {
      props: { preview: true },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })], stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        teleport: true,
      } },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Local preview')
    expect(sharedChannelAPI.list).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(wrapper.find('article').exists()).toBe(true))
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.find('article button').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    await wrapper.find('input[maxlength="100"]').setValue('Edited preview')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Edited preview')
    expect(sharedChannelAPI.save).not.toHaveBeenCalled()
  })

  it('saves time and budget as mutually exclusive conditions', async () => {
    const wrapper = mount(SharedChannelsView, {
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })], stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        teleport: true,
      } },
    })
    await flushPromises()
    await wrapper.find('header button').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(wrapper.find('button[form="shared-channel-form"]').exists()).toBe(true)
    await wrapper.find('input[maxlength="100"]').setValue('Shared service')
    wrapper.findComponent(SharedChannelModelPicker).vm.$emit('update:modelValue', ['model-a', 'model-b', 'Display name'])
    await wrapper.find('select').setValue('1')
    await wrapper.find('input[value="budget"]').setValue(true)
    expect(wrapper.find('input[type="datetime-local"]').exists()).toBe(false)
    await wrapper.find('input[placeholder="100"]').setValue('100')
    await wrapper.find('input[min="0.000001"]').setValue('2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(sharedChannelAPI.save).toHaveBeenLastCalledWith(null, expect.objectContaining({
      expires_at: null, budget_limit: 100, budget_multiplier: 2, models: ['model-a', 'model-b', 'Display name'],
    }))
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)

    await wrapper.find('header button').trigger('click')
    await wrapper.find('input[maxlength="100"]').setValue('Timed service')
    await wrapper.find('select').setValue('1')
    await wrapper.find('input[value="budget"]').setValue(true)
    await wrapper.find('input[placeholder="100"]').setValue('200')
    await wrapper.find('input[value="time"]').setValue(true)
    expect(wrapper.find('input[placeholder="100"]').exists()).toBe(false)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(sharedChannelAPI.save).toHaveBeenLastCalledWith(null, expect.objectContaining({
      expires_at: expect.any(String), budget_limit: null,
    }))
  })
  it('confirms deletion and keeps the last preview item removed after refresh', async () => {
    const wrapper = mount(SharedChannelsView, {
      props: { preview: true },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })], stubs: {
        AppLayout: { template: '<div><slot /></div>' }, teleport: true,
      } },
    })
    await flushPromises()
    const count = wrapper.findAll('article').length
    await wrapper.find('[data-testid="delete-shared-channel"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').text()).toContain('Existing usage and billing records are kept')
    await wrapper.findAll('button').find(button => button.text() === 'Cancel')!.trigger('click')
    expect(wrapper.findAll('article')).toHaveLength(count)
    for (let i = 0; i < count; i++) {
      await wrapper.find('[data-testid="delete-shared-channel"]').trigger('click')
      await wrapper.find('[data-testid="confirm-delete-shared"]').trigger('click')
      await flushPromises()
    }
    await wrapper.findAll('button').find(button => button.text() === 'Refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('article')).toHaveLength(0)
    expect(sharedChannelAPI.remove).not.toHaveBeenCalled()
  })

})
