<template>
  <AppLayout>
    <div class="mx-auto w-full min-w-0 max-w-[1540px] space-y-7 overflow-x-clip pb-12">
      <section class="w-full min-w-0 space-y-5" :aria-label="t('nav.sharedChannels')">
        <div class="flex flex-col gap-3 xl:flex-row xl:flex-wrap xl:items-center xl:justify-between">
          <div class="flex min-w-0 flex-1 flex-col gap-3 sm:flex-row sm:items-center">
            <label class="relative block w-full sm:max-w-[320px]">
              <span class="sr-only">{{ t('freeChannels.searchLabel') }}</span>
              <Icon name="search" size="sm" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-400" />
              <input v-model="searchQuery" data-testid="free-channel-search" type="search" :placeholder="t('freeChannels.searchPlaceholder')" class="input !rounded-[14px] !border-gray-200/90 !bg-white !py-2.5 !pl-10 text-xs shadow-sm dark:!border-dark-700 dark:!bg-dark-800" />
            </label>
            <div class="flex max-w-full items-center gap-1.5 overflow-x-auto rounded-[14px] border border-gray-200/80 bg-white p-1 shadow-sm dark:border-dark-700 dark:bg-dark-800" role="group" :aria-label="t('freeChannels.filterLabel')">
              <button v-for="option in filterOptions" :key="option.value" type="button" class="shrink-0 rounded-[10px] px-3 py-2 text-[11px] font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" :class="activeFilter === option.value ? 'bg-gray-900 text-white shadow-sm dark:bg-white dark:text-gray-900' : 'text-gray-500 hover:bg-gray-50 hover:text-gray-800 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white'" :aria-pressed="activeFilter === option.value" @click="activeFilter = option.value">
                {{ t(option.labelKey) }} <span class="ml-1 font-mono text-[9px] opacity-60">{{ option.count }}</span>
              </button>
            </div>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 text-[11px] text-gray-500 dark:text-dark-400">
            <dl class="flex flex-wrap items-center gap-x-3 gap-y-1">
              <div class="flex items-baseline gap-1.5"><dt>{{ t('freeChannels.summary.participants') }}</dt><dd class="font-mono font-semibold text-gray-700 dark:text-dark-200">{{ participantCount }}</dd></div>
              <div class="flex items-baseline gap-1.5"><dt>{{ t('freeChannels.summary.requests24h') }}</dt><dd class="font-mono font-semibold text-gray-700 dark:text-dark-200">{{ requestCountLabel }}</dd></div>
            </dl>
            <span>{{ t('freeChannels.resultCount', { count: filteredChannels.length }) }}</span>
          </div>
        </div>

        <div v-if="loading && !channels.length" class="rounded-[26px] border border-gray-200 bg-white py-16 text-center text-sm text-gray-400 dark:border-dark-700 dark:bg-dark-800">{{ t('common.loading') }}</div>
        <div v-else-if="loadError" role="alert" class="rounded-2xl border border-rose-200 bg-rose-50 px-5 py-4 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-300">{{ loadError }}</div>
        <div v-else-if="filteredChannels.length" class="grid w-full min-w-0 grid-cols-1 items-stretch gap-4 lg:grid-cols-2 2xl:grid-cols-3 2xl:gap-5">
          <FreeChannelCard v-for="channel in filteredChannels" :key="channel.id" :channel="channel" @join="openJoinDialog(channel)" />
        </div>
        <div v-else class="rounded-[26px] border border-dashed border-gray-300 bg-white/70 px-6 py-16 text-center dark:border-dark-600 dark:bg-dark-800/50">
          <span class="mx-auto grid h-12 w-12 place-items-center rounded-2xl bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-dark-300"><Icon name="search" size="lg" /></span>
          <h2 class="mt-4 text-sm font-semibold text-gray-800 dark:text-white">{{ t(searchQuery ? 'freeChannels.noSearchResults' : 'freeChannels.empty') }}</h2>
          <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">{{ t(searchQuery ? 'freeChannels.noSearchResultsHint' : 'freeChannels.emptyHint') }}</p>
          <button v-if="searchQuery || activeFilter !== 'all'" type="button" class="btn btn-secondary btn-sm mt-4" @click="clearFilters">{{ t('freeChannels.clearFilters') }}</button>
        </div>
      </section>

      <JoinFreeChannelDialog v-if="selectedChannel" :show="showJoinDialog" :channel="selectedChannel" :loading="creatingKey" :error="createError" :created-key="createdKey" :preview="previewMode" @close="closeJoinDialog" @confirm-preview="handleJoin" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useIntervalFn, useNow } from '@vueuse/core'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import FreeChannelCard from '@/components/user/free-channels/FreeChannelCard.vue'
import JoinFreeChannelDialog from '@/components/user/free-channels/JoinFreeChannelDialog.vue'
import * as sharedChannelsAPI from '@/api/sharedChannels'
import type { SharedChannel } from '@/api/sharedChannels'

type FilterValue = 'all' | 'open' | 'joined' | 'full'
type CardChannel = import('@/components/user/free-channels/freeChannelsMock').FreeChannelMock & { apiID?: number; currentShare?: number; budgetLimit?: number | null; budgetUsed?: number }
const { t, locale } = useI18n()
const channels = ref<CardChannel[]>([])
const searchQuery = ref('')
const activeFilter = ref<FilterValue>('all')
const selectedChannel = ref<CardChannel | null>(null)
const showJoinDialog = ref(false)
const loading = ref(false)
const creatingKey = ref(false)
const loadError = ref('')
const createError = ref('')
const createdKey = ref('')
const previewMode = import.meta.env.DEV && new URLSearchParams(window.location.search).get('preview') === '1'
const now = useNow({ interval: 1000 })
const liveChannels = computed(() => channels.value.filter(channel => channel.budgetLimit != null
  ? (channel.budgetUsed ?? 0) < channel.budgetLimit
  : !!channel.expiresAt && Date.parse(channel.expiresAt) > now.value.getTime()))

const filterOptions = computed(() => [
  { value: 'all' as const, labelKey: 'freeChannels.filters.all', count: liveChannels.value.length },
  { value: 'open' as const, labelKey: 'freeChannels.filters.open', count: countByAvailability('open') },
  { value: 'joined' as const, labelKey: 'freeChannels.filters.joined', count: countByAvailability('joined') },
  { value: 'full' as const, labelKey: 'freeChannels.filters.full', count: countByAvailability('full') },
])
const filteredChannels = computed(() => {
  const query = searchQuery.value.trim().toLocaleLowerCase(locale.value)
  return liveChannels.value.filter((channel) => {
    const matchesFilter = activeFilter.value === 'all' || channel.availability === activeFilter.value
    const searchable = `${channel.name} ${channel.provider} ${channel.models.join(' ')} ${channel.usernames.join(' ')}`.toLocaleLowerCase(locale.value)
    return matchesFilter && (!query || searchable.includes(query))
  })
})
const participantCount = computed(() => liveChannels.value.reduce((sum, channel) => sum + channel.usernames.length, 0))
const requestCount = computed(() => liveChannels.value.reduce((sum, channel) => sum + channel.hourlyStatuses.reduce((total, hour) => total + hour.requests, 0), 0))
const requestCountLabel = computed(() => new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(requestCount.value))

watch(liveChannels, (next) => {
  if (!selectedChannel.value) return
  const selected = next.find(channel => channel.id === selectedChannel.value?.id)
  if (selected) selectedChannel.value = selected
  else if (!creatingKey.value && !createdKey.value) closeJoinDialog()
})
useIntervalFn(() => {
  if (!previewMode && !loading.value) void load()
}, 15_000)

onMounted(async () => {
  if (previewMode) {
    const { freeChannelSamples } = await import('@/components/user/free-channels/freeChannelsMock')
    channels.value = freeChannelSamples.map(channel => ({ ...channel, personalConcurrency: 5, usernames: [...channel.usernames], hourlyStatuses: channel.hourlyStatuses.map(hour => ({ ...hour })) }))
    return
  }
  await load()
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    channels.value = (await sharedChannelsAPI.list()).map(toCardChannel)
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : t('freeChannels.loadFailed')
  } finally {
    loading.value = false
  }
}
function toCardChannel(channel: SharedChannel): CardChannel {
  const provider = channel.platform === 'gemini' ? 'gemini'
    : channel.platform === 'anthropic' ? 'anthropic'
      : channel.platform === 'deepseek' ? 'deepseek'
        : 'openai'
  const expiresAt = channel.expires_at ? new Date(channel.expires_at) : null
  const hourlyStatuses = Array.from({ length: 24 }, (_, index) => {
    const hour = channel.hours[index]
    return {
      hour: index,
      requests: hour?.requests ?? 0,
      failures: hour?.failures ?? 0,
      status: !hour || hour.requests === 0 ? 'idle' as const : hour.failures > 0 ? 'failure' as const : 'success' as const,
    }
  })
  const usernames = [...channel.usernames]
  return {
    id: String(channel.id),
    name: channel.name,
    provider,
    models: channel.models,
    availability: channel.joined ? 'joined' : usernames.length >= channel.max_concurrency ? 'full' : 'open',
    expiresAt: expiresAt?.toISOString() ?? null,
    budgetLimit: channel.budget_limit,
    budgetUsed: channel.budget_used,
    maxConcurrency: channel.max_concurrency,
    usernames,
    currentUserJoined: channel.joined,
    hourlyStatuses,
    apiID: channel.id,
    currentShare: channel.share,
  } as CardChannel
}
function countByAvailability(availability: FilterValue) {
  return liveChannels.value.filter(channel => channel.availability === availability).length
}
function openJoinDialog(channel: CardChannel) {
  selectedChannel.value = channel
  showJoinDialog.value = true
}
function handleJoin(name: string) {
  if (previewMode) {
    const channel = selectedChannel.value
    if (!channel) return
    if (!channel.currentUserJoined) {
      channel.currentUserJoined = true
      channel.availability = 'joined'
    }
    closeJoinDialog()
    return
  }
  void createChannelKey(name)
}
async function createChannelKey(name: string) {
  if (!selectedChannel.value || creatingKey.value) return
  creatingKey.value = true
  createError.value = ''
  try {
    if (!selectedChannel.value.apiID) throw new Error(t('freeChannels.createKeyFailed'))
    const channel = await sharedChannelsAPI.createKey(selectedChannel.value.apiID, name)
    createdKey.value = channel.key
    await load()
  } catch (cause) {
    createError.value = cause instanceof Error ? cause.message : t('freeChannels.createKeyFailed')
  } finally {
    creatingKey.value = false
  }
}
function closeJoinDialog() {
	if (creatingKey.value) return
  showJoinDialog.value = false
  selectedChannel.value = null
  createdKey.value = ''
  createError.value = ''
}
function clearFilters() {
  searchQuery.value = ''
  activeFilter.value = 'all'
}
</script>
