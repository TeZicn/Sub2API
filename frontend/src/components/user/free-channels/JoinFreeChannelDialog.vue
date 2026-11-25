<template>
  <BaseDialog
    :show="show"
    :title="t('freeChannels.joinDialog.title')"
    width="narrow"
    :close-on-click-outside="!loading"
    :close-on-escape="!loading"
    :show-close-button="!loading"
    @close="emit('close')"
  >
    <div data-testid="join-free-channel-dialog" class="space-y-5">
      <div v-if="preview" class="flex items-start gap-3 rounded-2xl border border-amber-200/80 bg-amber-50/80 p-3.5 dark:border-amber-800/50 dark:bg-amber-950/30">
        <Icon name="beaker" size="sm" class="mt-0.5 shrink-0 text-amber-600 dark:text-amber-300" />
        <p class="text-[11px] leading-5 text-amber-800/80 dark:text-amber-200/70">{{ t('freeChannels.joinDialog.previewNotice') }}</p>
      </div>
       <div class="flex items-center gap-3">
        <span class="grid h-10 w-10 place-items-center rounded-[14px] bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
          <ProviderIcon :provider="channel.provider" :size="20" />
        </span>
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ channel.name }}</p>
        </div>
      </div>

      <label v-if="!createdKey" class="block space-y-1.5">
        <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('freeChannels.joinDialog.keyName') }}</span>
        <input v-model.trim="keyName" class="input" maxlength="100" :disabled="loading" :placeholder="t('freeChannels.joinDialog.keyNamePlaceholder')" />
      </label>
      <div v-if="createdKey" class="space-y-2 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900/60 dark:bg-emerald-950/30">
        <p class="text-xs font-semibold text-emerald-800 dark:text-emerald-200">{{ t('freeChannels.joinDialog.keyCreated') }}</p>
        <p class="text-xs font-medium text-emerald-800 dark:text-emerald-200">{{ keyName }}</p>
        <p class="text-[10px] leading-4 text-emerald-700/80 dark:text-emerald-300/80">{{ t('freeChannels.joinDialog.keyCreatedHint') }}</p>
        <code class="block overflow-x-auto rounded-xl bg-white px-3 py-2 font-mono text-xs text-gray-800 dark:bg-dark-900 dark:text-dark-100">{{ createdKey }}</code>
        <button type="button" class="btn btn-secondary btn-sm" @click="copyKey">
          <Icon :name="copied ? 'check' : 'copy'" size="xs" />
          {{ copied ? t('common.copied') : t('common.copy') + ' Key' }}
        </button>
        <p v-if="copyError" role="alert" class="text-xs text-rose-600 dark:text-rose-300">{{ copyError }}</p>
      </div>
      <p v-if="error" role="alert" class="text-xs text-rose-600 dark:text-rose-300">{{ error }}</p>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="emit('close')">
        {{ t('common.cancel') }}
      </button>
      <button type="button" class="btn btn-primary" :disabled="loading || (!preview && !createdKey && !keyName.trim())" @click="preview ? emit('confirmPreview', '') : createdKey ? emit('close') : emit('confirmPreview', keyName)">
        <Icon :name="createdKey ? 'check' : loading ? 'refresh' : 'key'" size="sm" :class="loading ? 'animate-spin' : ''" />
        {{ preview ? t('freeChannels.joinDialog.previewAction') : createdKey ? t('freeChannels.joinDialog.doneAction') : loading ? t('common.loading') : t('freeChannels.joinDialog.createAction') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import type { FreeChannelMock } from './freeChannelsMock'

const props = defineProps<{
  show: boolean
  channel: FreeChannelMock
  loading?: boolean
  error?: string
  createdKey?: string
  preview?: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'confirmPreview', name: string): void
}>()

const { t } = useI18n()
const keyName = ref(props.channel.name)
const copied = ref(false)
const copyError = ref('')

async function copyKey() {
  if (!props.createdKey) return
  copyError.value = ''
  try {
    await navigator.clipboard.writeText(props.createdKey)
    copied.value = true
  } catch {
    copyError.value = t('common.copyFailed')
  }
}

</script>
