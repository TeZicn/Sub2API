<template>
  <AppLayout>
    <main class="mx-auto w-full max-w-[1440px] space-y-7 pb-12">
      <p v-if="previewMode" role="status" class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-2.5 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">{{ t('admin.sharedChannels.previewNotice') }}</p>
      <header class="flex flex-col gap-4 rounded-[26px] border border-gray-200/80 bg-white p-6 shadow-sm dark:border-dark-700 dark:bg-dark-800 sm:flex-row sm:items-end sm:justify-between sm:p-8">
        <div>
          <p class="text-[10px] font-bold uppercase tracking-[0.2em] text-primary-700 dark:text-primary-300">{{ t('admin.sharedChannels.eyebrow') }}</p>
          <h1 class="mt-2 text-2xl font-bold tracking-tight text-gray-900 dark:text-white">{{ t('admin.sharedChannels.title') }}</h1>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-500 dark:text-dark-300">{{ t('admin.sharedChannels.description') }}</p>
        </div>
        <button class="btn btn-primary shrink-0" type="button" @click="startCreate">
          <Icon name="plus" size="sm" />
          {{ t('admin.sharedChannels.create') }}
        </button>
      </header>

      <section v-if="editing" class="rounded-[24px] border border-primary-200/70 bg-white p-5 shadow-sm dark:border-primary-900/50 dark:bg-dark-800 sm:p-7">
        <div class="mb-6 flex items-start justify-between gap-4">
          <div>
            <h2 class="text-base font-bold text-gray-900 dark:text-white">{{ editing.id ? t('admin.sharedChannels.editTitle') : t('admin.sharedChannels.createTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.sharedChannels.formHint') }}</p>
          </div>
          <button class="btn btn-secondary btn-sm" type="button" @click="cancelEdit">{{ t('common.cancel') }}</button>
        </div>

        <form class="grid gap-4 md:grid-cols-2 xl:grid-cols-4" @submit.prevent="save">
          <label class="space-y-1.5 xl:col-span-2">
            <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.name') }}</span>
            <input v-model.trim="editing.name" class="input" maxlength="100" required :placeholder="t('admin.sharedChannels.namePlaceholder')" />
          </label>
          <label class="space-y-1.5 xl:col-span-2">
            <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.group') }}</span>
            <select v-model.number="editing.group_id" class="input" required>
              <option :value="0" disabled>{{ t('admin.sharedChannels.selectGroup') }}</option>
              <option v-for="group in eligibleGroups" :key="group.id" :value="group.id">{{ group.name }} · {{ group.platform }}</option>
              <option v-if="editing.group_id && !eligibleGroups.some(group => group.id === editing?.group_id)" :value="editing.group_id" disabled>{{ editing.group_name }} · {{ t('admin.sharedChannels.groupUnavailable') }}</option>
            </select>
            <span class="block text-[10px] leading-4 text-gray-400 dark:text-dark-400">{{ t('admin.sharedChannels.groupHint') }}</span>
          </label>
          <label class="space-y-1.5">
            <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.limit') }}</span>
            <input v-model.number="editing.max_concurrency" class="input" type="number" min="1" max="1000" required />
          </label>
          <fieldset class="space-y-1.5 md:col-span-2 xl:col-span-3">
            <legend class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.endMode') }}</legend>
            <div class="flex max-w-md gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-900">
              <label v-for="mode in ['time', 'budget'] as const" :key="mode" class="relative flex flex-1 cursor-pointer items-center justify-center gap-2 rounded-lg px-4 py-2 text-xs font-semibold transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-primary-500" :class="endMode === mode ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-500 dark:text-dark-400'">
                <input v-model="endMode" type="radio" name="shared-service-end-mode" :value="mode" class="sr-only" />
                {{ t(mode === 'time' ? 'admin.sharedChannels.timeMode' : 'admin.sharedChannels.budgetMode') }}
              </label>
            </div>
          </fieldset>
          <label v-if="endMode === 'time'" class="space-y-1.5 md:col-span-2">
            <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.expiry') }}</span>
            <input v-model="expiryInput" class="input" type="datetime-local" :required="editing.published" />
          </label>
          <label v-if="endMode === 'budget'" class="space-y-1.5">
            <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.budgetLimit') }}</span>
            <input v-model.number="editing.budget_limit" class="input" type="number" min="0.00000001" max="1000000000" step="any" placeholder="100" required />
            <span class="block text-[10px] leading-4 text-gray-400 dark:text-dark-400">{{ t('admin.sharedChannels.budgetHint') }}</span>
          </label>
          <label v-if="endMode === 'budget'" class="space-y-1.5">
            <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.budgetMultiplier') }}</span>
            <input v-model.number="editing.budget_multiplier" class="input" type="number" min="0.000001" max="1000" step="any" required />
          </label>
          <label class="flex items-center gap-3 rounded-2xl border border-gray-200/80 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/40 md:col-span-2">
            <input v-model="editing.published" type="checkbox" class="h-4 w-4 rounded accent-primary-600" />
            <span>
              <span class="block text-xs font-semibold text-gray-800 dark:text-dark-100">{{ t('admin.sharedChannels.publishNow') }}</span>
              <span class="mt-0.5 block text-[10px] text-gray-500 dark:text-dark-400">{{ t('admin.sharedChannels.publishHint') }}</span>
            </span>
          </label>
          <div class="flex items-center justify-end gap-2 md:col-span-2 xl:col-span-4">
            <p v-if="error" role="alert" class="mr-auto text-xs text-rose-600 dark:text-rose-300">{{ error }}</p>
            <button class="btn btn-secondary" type="button" :disabled="saving" @click="cancelEdit">{{ t('common.cancel') }}</button>
            <button class="btn btn-primary" type="submit" :disabled="saving">
              <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
              {{ saving ? t('common.saving') : t(previewMode ? 'admin.sharedChannels.savePreview' : 'admin.sharedChannels.save') }}
            </button>
          </div>
        </form>
      </section>

      <section class="space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-bold text-gray-800 dark:text-dark-100">{{ t('admin.sharedChannels.listTitle') }}</h2>
          <button class="btn btn-secondary btn-sm" type="button" :disabled="loading" @click="load">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('common.refresh') }}
          </button>
        </div>

        <div v-if="loadError" role="alert" class="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-300">{{ loadError }}</div>

        <div v-if="loading && !channels.length" class="rounded-[24px] border border-gray-200/80 bg-white p-12 text-center text-sm text-gray-400 dark:border-dark-700 dark:bg-dark-800">{{ t('common.loading') }}</div>
        <div v-else-if="!channels.length" class="rounded-[24px] border border-dashed border-gray-300 bg-white px-6 py-16 text-center dark:border-dark-700 dark:bg-dark-800">
          <span class="mx-auto grid h-12 w-12 place-items-center rounded-2xl bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"><Icon name="users" size="lg" /></span>
          <h3 class="mt-4 text-sm font-bold text-gray-800 dark:text-white">{{ t('admin.sharedChannels.emptyTitle') }}</h3>
          <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.sharedChannels.emptyHint') }}</p>
        </div>

        <div v-else class="grid gap-3 xl:grid-cols-2">
          <article v-for="item in channels" :key="item.id" class="rounded-[22px] border border-gray-200/80 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-800">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="truncate text-sm font-bold text-gray-900 dark:text-white">{{ item.name }}</h3>
                  <span class="rounded-full px-2.5 py-1 text-[10px] font-semibold" :class="isAvailable(item) ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'">
                    {{ t(!item.published ? 'admin.sharedChannels.unpublished' : isAvailable(item) ? 'admin.sharedChannels.published' : 'admin.sharedChannels.ended') }}
                  </span>
                </div>
                <p class="mt-1.5 truncate text-xs text-gray-500 dark:text-dark-300">{{ item.group_name }} · {{ item.platform }}</p>
              </div>
              <span class="inline-flex shrink-0 items-center gap-1.5 rounded-xl bg-primary-50 px-3 py-2 text-[11px] font-semibold text-primary-800 dark:bg-primary-900/30 dark:text-primary-200">
                <Icon name="users" size="xs" />
                {{ item.max_concurrency }} {{ t('admin.sharedChannels.requests') }}
              </span>
            </div>
            <div class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-4 dark:border-dark-700">
              <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-gray-500 dark:text-dark-400">
                <template v-if="item.budget_limit == null">
                  {{ t('admin.sharedChannels.expiry') }} ·
                  <span class="font-medium text-gray-700 dark:text-dark-200">{{ formatExpiry(item.expires_at) }}</span>
                </template>
                <span v-if="!item.group_eligible" class="ml-2 text-amber-700 dark:text-amber-300">{{ t('admin.sharedChannels.groupUnavailable') }}</span>
                <span v-if="item.budget_limit != null" class="font-mono tabular-nums">
                  {{ t('admin.sharedChannels.budgetProgress', { used: formatMoney(item.budget_used), limit: formatMoney(item.budget_limit) }) }}
                </span>
                <span v-if="item.budget_pricing_blocked" class="font-semibold text-rose-600 dark:text-rose-300">{{ t('admin.sharedChannels.pricingBlocked') }}</span>
                <span v-else-if="item.budget_limit != null && item.budget_used >= item.budget_limit" class="font-semibold text-amber-700 dark:text-amber-300">{{ t('admin.sharedChannels.budgetExhausted') }}</span>
              </div>
              <div class="flex items-center gap-2">
                <button class="btn btn-secondary btn-sm" type="button" @click="startEdit(item)">{{ t('common.edit') }}</button>
                <button v-if="item.published" class="btn btn-danger btn-sm" type="button" @click="unpublish(item)">{{ t('admin.sharedChannels.unpublish') }}</button>
              </div>
            </div>
          </article>
        </div>
      </section>
    </main>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import * as groupsAPI from '@/api/admin/groups'
import * as sharedChannelAPI from '@/api/admin/sharedChannels'
import type { AdminGroup } from '@/types'
import type { AdminSharedChannel, SharedChannelInput } from '@/api/admin/sharedChannels'

type EditingChannel = SharedChannelInput & { id: number | null; group_name?: string }
type SharedGroup = Pick<AdminGroup, 'id' | 'name' | 'platform' | 'status' | 'is_exclusive' | 'subscription_type'>
const props = defineProps<{ preview?: boolean }>()
const previewMode = import.meta.env.DEV && props.preview === true
const { t, locale } = useI18n()
const channels = ref<AdminSharedChannel[]>([])
const groups = ref<SharedGroup[]>([])
const editing = ref<EditingChannel | null>(null)
const expiryInput = ref('')
const endMode = ref<'time' | 'budget'>('time')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const loadError = ref('')
const eligibleGroups = computed(() => groups.value.filter(group =>
  group.status === 'active' && group.is_exclusive && group.subscription_type === 'standard',
))

onMounted(async () => {
  await load()
  if (previewMode && channels.value[0]) startEdit(channels.value[0])
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    if (previewMode) {
      if (!channels.value.length) {
        const samples = await import('./sharedChannelsPreview')
        channels.value = samples.channels.map(channel => ({ ...channel }))
        groups.value = samples.groups.map(group => ({ ...group }))
      }
      return
    }
    const [nextChannels, nextGroups] = await Promise.all([sharedChannelAPI.list(), groupsAPI.getAll()])
    channels.value = nextChannels
    groups.value = nextGroups
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : t('admin.sharedChannels.loadFailed')
  } finally {
    loading.value = false
  }
}

function toLocalInput(value: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function startCreate() {
  error.value = ''
  endMode.value = 'time'
  editing.value = {
    id: null,
    name: '',
    group_id: 0,
    max_concurrency: 5,
    expires_at: null,
    published: true,
    budget_limit: null,
    budget_multiplier: 1,
  }
  const expiry = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000)
  expiryInput.value = toLocalInput(expiry.toISOString())
}

function startEdit(item: AdminSharedChannel) {
  error.value = ''
  endMode.value = item.budget_limit == null ? 'time' : 'budget'
  editing.value = { ...item }
  expiryInput.value = toLocalInput(item.expires_at)
}

function cancelEdit() {
  editing.value = null
  expiryInput.value = ''
  error.value = ''
}

async function save() {
  if (!editing.value) return
  saving.value = true
  error.value = ''
  try {
    const input: SharedChannelInput = {
      name: editing.value.name,
      group_id: Number(editing.value.group_id),
      max_concurrency: Number(editing.value.max_concurrency),
      expires_at: endMode.value === 'time' && expiryInput.value ? new Date(expiryInput.value).toISOString() : null,
      published: editing.value.published,
      budget_limit: endMode.value === 'budget' && editing.value.budget_limit != null && String(editing.value.budget_limit).trim() !== '' ? Number(editing.value.budget_limit) : null,
      budget_multiplier: Number(editing.value.budget_multiplier),
    }
    await persist(editing.value.id, input)
    cancelEdit()
    await load()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('admin.sharedChannels.saveFailed')
  } finally {
    saving.value = false
  }
}

function formatExpiry(value: string | null) {
  if (!value) return t('freeChannels.noExpiryDate')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function formatMoney(value: number) {
  return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value)
}

function isAvailable(item: AdminSharedChannel) {
  return item.published && item.group_eligible && !item.budget_pricing_blocked
    && (!item.expires_at || Date.parse(item.expires_at) > Date.now())
    && (item.budget_limit == null || item.budget_used < item.budget_limit)
}

async function unpublish(item: AdminSharedChannel) {
  const ok = previewMode || window.confirm(t('admin.sharedChannels.unpublishConfirm'))
  if (!ok) return
  try {
    await persist(item.id, {
      name: item.name,
      group_id: item.group_id,
      max_concurrency: item.max_concurrency,
      expires_at: item.expires_at,
      published: false,
      budget_limit: item.budget_limit,
      budget_multiplier: item.budget_multiplier,
    })
    await load()
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : t('admin.sharedChannels.saveFailed')
  }
}

async function persist(id: number | null, input: SharedChannelInput) {
  if (!previewMode) return sharedChannelAPI.save(id, input)
  const group = groups.value.find(item => item.id === input.group_id)
  if (!group) throw new Error(t('admin.sharedChannels.selectGroup'))
  const existing = channels.value.find(item => item.id === id)
  const item: AdminSharedChannel = {
    ...input,
    id: id ?? Math.max(0, ...channels.value.map(channel => channel.id)) + 1,
    group_name: group.name, platform: group.platform, group_eligible: true,
    budget_used: existing?.budget_used ?? 0, budget_pricing_blocked: false,
  }
  channels.value = existing ? channels.value.map(channel => channel.id === id ? item : channel) : [item, ...channels.value]
  return item
}
</script>
