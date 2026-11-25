<template>
  <div class="min-w-0 w-full space-y-2.5">
    <div class="grid grid-cols-[repeat(24,minmax(0,1fr))] gap-1" role="list" :aria-label="t('freeChannels.historyLabel')">
      <span
        v-for="bucket in statuses"
        :key="bucket.hour"
        role="img"
        tabindex="0"
        data-testid="hour-status-cell"
        class="h-7 min-w-0 rounded-[5px] outline-none transition duration-150 hover:-translate-y-0.5 focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800 sm:h-8"
        :class="statusClass(bucket.status)"
        :title="tooltip(bucket)"
        :aria-label="tooltip(bucket)"
      />
    </div>
    <div class="flex items-center justify-between text-[10px] font-medium tracking-wide text-gray-400 dark:text-dark-400">
      <span>{{ t('freeChannels.hoursAgo24') }}</span>
      <span>12h</span>
      <span>{{ t('freeChannels.now') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { FreeChannelMock, HourlyRequestStatus } from './freeChannelsMock'

defineProps<{
  statuses: FreeChannelMock['hourlyStatuses']
}>()

const { t } = useI18n()

function statusClass(status: HourlyRequestStatus['status']) {
  if (status === 'success') return 'bg-emerald-500 hover:bg-emerald-600 focus-visible:ring-emerald-500'
  if (status === 'failure') return 'bg-rose-500 hover:bg-rose-600 focus-visible:ring-rose-500'
  return 'bg-gray-200 hover:bg-gray-300 focus-visible:ring-gray-400 dark:bg-dark-600 dark:hover:bg-dark-500'
}

function tooltip(bucket: HourlyRequestStatus) {
  if (bucket.status === 'idle') return t('freeChannels.hourTooltipIdle', {
    start: 23 - bucket.hour,
    end: 24 - bucket.hour,
  })
  return t('freeChannels.hourTooltip', {
    start: 23 - bucket.hour,
    end: 24 - bucket.hour,
    requests: bucket.requests,
    failures: bucket.failures,
  })
}
</script>
