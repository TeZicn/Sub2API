<template>
  <article
    data-testid="free-channel-card"
    class="group relative flex h-full w-full min-w-0 max-w-full flex-col overflow-hidden rounded-[26px] border border-gray-200/80 bg-white shadow-[0_14px_44px_-30px_rgba(15,52,55,0.38)] transition duration-300 hover:-translate-y-1 hover:border-primary-200 hover:shadow-[0_22px_55px_-30px_rgba(15,52,55,0.42)] dark:border-dark-700 dark:bg-dark-800/90 dark:hover:border-primary-700"
  >
    <div class="pointer-events-none absolute inset-x-8 top-0 h-px bg-gradient-to-r from-transparent via-primary-300/80 to-transparent dark:via-primary-700/70" />

    <div class="flex w-full min-w-0 flex-1 flex-col p-4 sm:p-6">
      <header class="flex min-w-0 items-start gap-3.5">
        <span
          class="grid h-12 w-12 shrink-0 place-items-center rounded-[17px] ring-1 ring-black/[0.04] dark:ring-white/[0.06]"
          :class="providerTileClass"
          aria-hidden="true"
        >
          <ProviderIcon :provider="channel.provider" :size="24" />
        </span>

        <div class="min-w-0 flex-1 pt-0.5">
          <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span class="text-[10px] font-bold uppercase tracking-[0.16em] text-gray-400 dark:text-dark-400">
              {{ providerLabel }}
            </span>
            <span class="h-1 w-1 rounded-full bg-gray-300 dark:bg-dark-600" aria-hidden="true" />
            <span class="text-[10px] font-semibold tracking-wide text-primary-700 dark:text-primary-300">
              {{ t('freeChannels.sharedBadge') }}
            </span>
          </div>
          <h2 class="mt-1 truncate text-[17px] font-bold tracking-[-0.025em] text-gray-900 dark:text-white" :title="channel.name">
            {{ channel.name }}
          </h2>
        </div>

        <span
          class="inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-1 text-[10px] font-semibold"
          :class="availabilityClass"
        >
          <span class="h-1.5 w-1.5 rounded-full" :class="availabilityDotClass" aria-hidden="true" />
          {{ t(`freeChannels.availability.${channel.availability}`) }}
        </span>
      </header>

      <section class="mt-5 min-w-0" :aria-label="t('freeChannels.openModels')">
        <div class="flex items-center justify-between gap-3">
          <h3 class="text-[11px] font-semibold text-gray-500 dark:text-dark-300">{{ t('freeChannels.openModels') }}</h3>
          <span class="font-mono text-[10px] tabular-nums text-gray-400 dark:text-dark-400">{{ channel.models.length }}</span>
        </div>
        <div class="mt-2 flex flex-wrap gap-1.5">
          <span v-for="model in visibleModels" :key="model" data-testid="shared-channel-model" class="max-w-full break-all rounded-lg border border-gray-200/80 bg-gray-50/80 px-2 py-1 font-mono text-[10px] leading-4 text-gray-600 dark:border-dark-600/70 dark:bg-dark-900/35 dark:text-dark-200">{{ model }}</span>
          <button v-if="channel.models.length > 3" type="button" class="rounded-lg px-2 py-1 text-[10px] font-semibold text-primary-700 transition-colors hover:bg-primary-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-300 dark:hover:bg-primary-900/30" :aria-expanded="modelsExpanded" @click="modelsExpanded = !modelsExpanded">
            {{ modelsExpanded ? t('freeChannels.collapseModels') : t('freeChannels.moreModels', { count: channel.models.length - 3 }) }}
          </button>
          <p v-if="!channel.models.length" class="text-[10px] text-gray-400 dark:text-dark-400">{{ t('freeChannels.noModels') }}</p>
        </div>
      </section>

      <div v-if="channel.budgetLimit == null" class="mt-5 flex min-w-0 items-center justify-between gap-2 rounded-2xl border border-gray-100 bg-gray-50/90 px-3 py-3 dark:border-dark-700/80 dark:bg-dark-900/45 sm:px-3.5">
        <div class="flex min-w-0 items-center gap-2.5">
          <span class="grid h-8 w-8 place-items-center rounded-xl bg-white text-gray-500 shadow-sm dark:bg-dark-800 dark:text-dark-300">
            <Icon name="clock" size="sm" />
          </span>
          <div class="min-w-0">
            <p class="text-[10px] font-medium text-gray-400 dark:text-dark-400">{{ t('freeChannels.publishedRemaining') }}</p>
            <p class="mt-0.5 font-mono text-xs font-semibold tabular-nums" :class="expiryTone">
              {{ expiryLabel }}
            </p>
          </div>
        </div>
        <span class="text-[10px] font-medium text-gray-400 dark:text-dark-400">{{ expiresOn }}</span>
      </div>

      <div v-else class="mt-5 rounded-2xl border border-gray-100 bg-gray-50/90 px-3.5 py-3 dark:border-dark-700/80 dark:bg-dark-900/45">
        <div class="flex flex-wrap items-end justify-between gap-2">
          <div>
            <p class="text-[10px] font-medium text-gray-400 dark:text-dark-400">{{ t('freeChannels.budgetRemaining') }}</p>
            <p class="mt-1 font-mono text-xl font-semibold tracking-tight tabular-nums" :class="budgetProgress >= 80 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-gray-100'">{{ formatMoney(Math.max(0, channel.budgetLimit - (channel.budgetUsed ?? 0))) }}</p>
          </div>
          <p class="text-[10px] text-gray-500 dark:text-dark-300">{{ t('freeChannels.budgetSpent', { used: formatMoney(channel.budgetUsed ?? 0), total: formatMoney(channel.budgetLimit) }) }}</p>
        </div>
        <div class="mt-2.5 h-1 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700" role="progressbar" :aria-valuenow="Math.min(100, budgetProgress)" aria-valuemin="0" aria-valuemax="100" :aria-label="t('freeChannels.budgetUsage')">
          <span class="block h-full rounded-full transition-[width]" :class="budgetProgress >= 80 ? 'bg-amber-500' : 'bg-primary-500'" :style="{ width: budgetProgress + '%' }" />
        </div>
      </div>

      <section class="mt-5 min-w-0" :aria-label="t('freeChannels.historyLabel')">
        <div class="mb-3 flex items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <span class="grid h-7 w-7 place-items-center rounded-lg bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              <Icon name="chart" size="xs" />
            </span>
            <div>
              <h3 class="text-xs font-bold text-gray-800 dark:text-gray-100">{{ t('freeChannels.historyTitle') }}</h3>
              <p class="mt-0.5 text-[10px] text-gray-400 dark:text-dark-400">{{ t('freeChannels.historySource') }}</p>
            </div>
          </div>
          <div class="flex shrink-0 items-center gap-2.5 text-[9px] font-medium text-gray-400 dark:text-dark-400" aria-hidden="true">
            <span class="inline-flex items-center gap-1"><i class="h-1.5 w-1.5 rounded-full bg-emerald-500" />{{ t('freeChannels.successShort') }}</span>
            <span class="inline-flex items-center gap-1"><i class="h-1.5 w-1.5 rounded-full bg-rose-500" />{{ t('freeChannels.failureShort') }}</span>
          </div>
        </div>
        <div class="min-w-0 rounded-2xl border border-gray-100 bg-gray-50/70 px-2.5 pb-2.5 pt-3 dark:border-dark-700/80 dark:bg-dark-900/35 sm:px-4">
          <FreeChannelStatusGrid :statuses="channel.hourlyStatuses" />
        </div>
      </section>

      <section class="mt-5 min-w-0 rounded-2xl border border-primary-100/80 bg-gradient-to-br from-primary-50/80 via-white to-white p-3.5 dark:border-primary-900/50 dark:from-primary-950/35 dark:via-dark-800 dark:to-dark-800 sm:p-4">
        <div class="flex items-start justify-between gap-3">
          <div>
            <p class="text-[10px] font-semibold uppercase tracking-[0.14em] text-primary-800/65 dark:text-primary-200/65">{{ t('freeChannels.concurrency') }}</p>
            <div class="mt-1 flex items-baseline gap-1.5">
              <span class="font-mono text-[28px] font-bold leading-none tracking-[-0.06em] text-gray-900 dark:text-white">{{ channel.maxConcurrency }}</span>
              <span class="text-[11px] font-medium text-gray-500 dark:text-dark-300">{{ t('freeChannels.slots') }}</span>
            </div>
          </div>
          <span class="inline-flex shrink-0 items-center gap-1.5 rounded-xl bg-white/80 px-2.5 py-2 text-[11px] font-semibold text-gray-600 shadow-sm ring-1 ring-primary-100/80 dark:bg-dark-800/80 dark:text-dark-200 dark:ring-primary-900/50">
            <Icon name="users" size="xs" class="text-primary-700 dark:text-primary-300" />
            <span class="font-mono tabular-nums">{{ channel.usernames.length }}</span>
            {{ t('freeChannels.peopleUsing') }}
          </span>
        </div>

        <div class="mt-3.5 grid gap-1.5" :style="{ gridTemplateColumns: `repeat(${meterSegments}, minmax(0, 1fr))` }" :aria-label="shareMeterLabel">
          <span
            v-for="slot in meterSegments"
            :key="slot"
            class="h-1.5 rounded-full transition-colors"
            :class="shareSegmentClass(slot - 1)"
            aria-hidden="true"
          />
        </div>

      </section>

      <section ref="participantsRef" class="mt-5 min-h-[61px] min-w-0" :aria-label="t('freeChannels.participants')" @keydown.esc="selectedUsername = null">
        <div class="flex items-center justify-between gap-2">
          <h3 class="text-[11px] font-semibold text-gray-600 dark:text-dark-200">{{ t('freeChannels.participants') }}</h3>
          <span v-if="channel.usernames.length" class="text-[10px] tabular-nums text-gray-400 dark:text-dark-400">
            {{ t('freeChannels.peopleCount', { count: channel.usernames.length }) }}
          </span>
        </div>
        <div v-if="channel.usernames.length" class="mt-2.5 flex flex-wrap items-center gap-1.5">
          <div
            v-for="(username, index) in visibleUsernames"
            :key="username"
            class="relative"
          >
            <button
              type="button"
              data-testid="shared-user-avatar"
              class="grid h-8 w-8 place-items-center rounded-full text-[11px] font-bold ring-2 ring-white transition-transform hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-primary-500 dark:ring-dark-800"
              :class="avatarClass(index)"
              :aria-label="t('freeChannels.viewUser', { name: username })"
              :aria-expanded="selectedUsername === username"
              :aria-describedby="selectedUsername === username ? `shared-user-${channel.id}-${index}` : undefined"
              @click="selectedUsername = selectedUsername === username ? null : username"
            >{{ avatarInitial(username) }}</button>
            <div v-if="selectedUsername === username" :id="`shared-user-${channel.id}-${index}`" role="tooltip" class="absolute bottom-full left-0 z-20 mb-2 w-max max-w-[220px] break-all rounded-xl border border-gray-200 bg-white px-3 py-2 text-xs font-medium text-gray-800 shadow-lg dark:border-dark-600 dark:bg-dark-700 dark:text-white">{{ username }}</div>
          </div>
          <button v-if="hiddenUserCount" type="button" class="h-8 rounded-full bg-gray-100 px-2.5 text-[10px] font-medium text-gray-500 hover:bg-gray-200 dark:bg-dark-700 dark:text-dark-300" :aria-label="t('freeChannels.morePeople', { count: hiddenUserCount })" @click="participantsExpanded = true">+{{ hiddenUserCount }}</button>
        </div>
        <p v-else class="mt-2.5 text-[10px] text-gray-400 dark:text-dark-400">{{ t('freeChannels.noParticipants') }}</p>
      </section>

      <footer class="mt-auto pt-5">
        <button
          type="button"
          class="group/join relative inline-flex w-full items-center justify-center gap-2.5 rounded-[14px] border px-4 py-3 text-[12px] font-semibold transition-all duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800"
          :class="buttonClass"
          :data-testid="`join-free-channel-${channel.id}`"
          @click="emit('join')"
        >
          <span class="grid h-6 w-6 place-items-center rounded-lg transition-colors" :class="channel.availability === 'open' ? 'bg-white/80 text-primary-700 shadow-sm dark:bg-primary-900/50 dark:text-primary-200' : 'bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-dark-400'">
            <Icon :name="buttonIcon" size="sm" />
          </span>
          <span>{{ buttonLabel }}</span>
          <Icon name="arrowRight" size="xs" class="absolute right-4 opacity-60 transition-transform group-hover/join:translate-x-0.5 group-hover/join:opacity-100" />
        </button>
      </footer>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { onClickOutside, useNow } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { estimateConcurrencyShare, type FreeChannelMock } from './freeChannelsMock'
import FreeChannelStatusGrid from './FreeChannelStatusGrid.vue'

const props = defineProps<{
  channel: FreeChannelMock & { currentShare?: number }
}>()

const emit = defineEmits<{
  (event: 'join'): void
}>()

const { t, locale } = useI18n()
const now = useNow({ interval: 1000 })
const modelsExpanded = ref(false)
const participantsExpanded = ref(false)
const participantsRef = ref<HTMLElement | null>(null)
const selectedUsername = ref<string | null>(null)
onClickOutside(participantsRef, () => { selectedUsername.value = null })
const meterSegments = computed(() => Math.min(20, props.channel.maxConcurrency))
const visibleModels = computed(() => modelsExpanded.value ? props.channel.models : props.channel.models.slice(0, 3))

const providerLabel = computed(() => {
  const key = `freeChannels.providers.${props.channel.provider}`
  return t(key)
})

const providerTileClass = computed(() => {
  const classes: Record<string, string> = {
    gemini: 'bg-gradient-to-br from-sky-50 via-white to-amber-50 text-sky-600 dark:from-sky-900/40 dark:via-dark-800 dark:to-amber-900/20 dark:text-sky-300',
    anthropic: 'bg-gradient-to-br from-orange-50 via-white to-rose-50 text-orange-700 dark:from-orange-900/30 dark:via-dark-800 dark:to-rose-900/20 dark:text-orange-300',
    deepseek: 'bg-gradient-to-br from-cyan-50 via-white to-blue-50 text-cyan-700 dark:from-cyan-900/30 dark:via-dark-800 dark:to-blue-900/20 dark:text-cyan-300',
    openai: 'bg-gradient-to-br from-emerald-50 via-white to-teal-50 text-emerald-700 dark:from-emerald-900/30 dark:via-dark-800 dark:to-teal-900/20 dark:text-emerald-300',
  }
  return classes[props.channel.provider] ?? 'bg-gray-50 text-gray-500 dark:bg-dark-700 dark:text-dark-200'
})

const availabilityClass = computed(() => {
  if (props.channel.availability === 'open') return 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-100 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/20'
  if (props.channel.availability === 'joined') return 'bg-primary-50 text-primary-700 ring-1 ring-primary-100 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-500/20'
  return 'bg-gray-100 text-gray-500 ring-1 ring-gray-200 dark:bg-dark-700 dark:text-dark-300 dark:ring-dark-600'
})

const availabilityDotClass = computed(() => {
  if (props.channel.availability === 'open') return 'bg-emerald-500'
  if (props.channel.availability === 'joined') return 'bg-primary-500'
  return 'bg-gray-400 dark:bg-dark-400'
})

const shareEstimate = computed(() => {
  if (props.channel.currentShare != null) return { min: props.channel.currentShare, max: props.channel.currentShare }
  const participantsBeforeJoin = props.channel.currentUserJoined
    ? Math.max(0, props.channel.usernames.length - 1)
    : props.channel.usernames.length
  return estimateConcurrencyShare(props.channel.maxConcurrency, participantsBeforeJoin, props.channel.personalConcurrency)
})

const shareMeterLabel = computed(() => {
  if (!shareEstimate.value) return t('freeChannels.capacityFull')
  return t('freeChannels.shareMeterLabel', {
    min: shareEstimate.value.min,
    max: shareEstimate.value.max,
    total: props.channel.maxConcurrency,
  })
})

const expiryMs = computed(() => props.channel.expiresAt ? Date.parse(props.channel.expiresAt) - now.value.getTime() : null)
const expiryTone = computed(() => expiryMs.value !== null && expiryMs.value < 24 * 60 * 60 * 1000
  ? 'text-amber-700 dark:text-amber-300'
  : 'text-gray-900 dark:text-gray-100')

const expiryLabel = computed(() => {
  if (expiryMs.value === null) return t('freeChannels.longTerm')
  if (expiryMs.value <= 0) return t('freeChannels.expired')
  const hours = Math.ceil(expiryMs.value / (60 * 60 * 1000))
  const days = Math.floor(hours / 24)
  const remainderHours = hours % 24
  if (days === 0) return t('freeChannels.hoursRemaining', { count: hours })
  if (remainderHours === 0) return t('freeChannels.daysRemaining', { count: days })
  return t('freeChannels.daysAndHoursRemaining', { days, hours: remainderHours })
})

const expiresOn = computed(() => {
  if (!props.channel.expiresAt) return t('freeChannels.noExpiryDate')
  const date = new Date(props.channel.expiresAt)
  if (Number.isNaN(date.getTime())) return t('freeChannels.noExpiryDate')
  return new Intl.DateTimeFormat(locale.value, { month: 'short', day: 'numeric' }).format(date)
})

const budgetProgress = computed(() => {
  if (props.channel.budgetLimit == null || props.channel.budgetLimit <= 0) return 0
  return Math.max(0, Math.min(100, ((props.channel.budgetUsed ?? 0) / props.channel.budgetLimit) * 100))
})

function formatMoney(value: number | null | undefined) {
  return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value ?? 0)
}

const visibleUsernames = computed(() => participantsExpanded.value ? props.channel.usernames : props.channel.usernames.slice(0, 8))
const hiddenUserCount = computed(() => Math.max(0, props.channel.usernames.length - visibleUsernames.value.length))

const buttonLabel = computed(() => {
  if (props.channel.availability === 'joined') return t('freeChannels.createAnotherKey')
  return t('freeChannels.joinAndCreateKey')
})

const buttonIcon = computed(() => props.channel.availability === 'joined' ? 'key' : 'plus')
const buttonClass = 'border-primary-200 bg-primary-50/90 text-primary-800 shadow-sm hover:-translate-y-0.5 hover:border-primary-300 hover:bg-primary-100 hover:shadow-md active:translate-y-0 dark:border-primary-800/80 dark:bg-primary-950/45 dark:text-primary-200 dark:hover:border-primary-700 dark:hover:bg-primary-900/50'

function shareSegmentClass(index: number) {
  if (!shareEstimate.value) return 'bg-gray-300 dark:bg-dark-600'
  const amount = index * props.channel.maxConcurrency / meterSegments.value
  if (amount < shareEstimate.value.min) return 'bg-primary-600 dark:bg-primary-400'
  if (amount < shareEstimate.value.max) return 'bg-primary-300 dark:bg-primary-700'
  return 'bg-gray-200 dark:bg-dark-700'
}

function avatarClass(index: number) {
  const classes = [
    'bg-primary-100 text-primary-800 dark:bg-primary-900/60 dark:text-primary-200',
    'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-200',
    'bg-violet-100 text-violet-700 dark:bg-violet-900/50 dark:text-violet-200',
  ]
  return classes[index % classes.length]
}

function avatarInitial(username: string) {
  return username.slice(0, 1).toUpperCase()
}
</script>
