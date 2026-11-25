<template>
  <BaseDialog
    :show="show"
    :title="t('freeChannels.joinDialog.title')"
    width="normal"
    :close-on-click-outside="!loading"
    :close-on-escape="!loading"
    :show-close-button="!loading"
    @close="emit('close')"
  >
    <div data-testid="join-free-channel-dialog" class="space-y-6">
      <div class="flex items-center gap-3.5 rounded-2xl border border-gray-200/80 bg-gray-50/80 p-4 dark:border-dark-700 dark:bg-dark-900/40">
        <span class="grid h-12 w-12 shrink-0 place-items-center rounded-[14px] border border-gray-200/70 bg-white text-gray-800 shadow-sm dark:border-dark-600 dark:bg-dark-800 dark:text-dark-100">
          <ProviderIcon :provider="channel.provider" :size="24" />
        </span>
        <p class="min-w-0 break-words text-base font-semibold leading-6 text-gray-900 dark:text-white">{{ channel.name }}</p>
      </div>

      <div v-if="preview" class="flex items-start gap-2.5 text-xs leading-5 text-amber-700 dark:text-amber-300">
        <Icon name="beaker" size="sm" class="mt-0.5 shrink-0" />
        <p>{{ t('freeChannels.joinDialog.previewNotice') }}</p>
      </div>

      <label v-if="!createdKey" class="block space-y-2">
        <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('freeChannels.joinDialog.keyName') }}</span>
        <input v-model.trim="keyName" class="input !rounded-xl !py-3" maxlength="100" :disabled="loading" :placeholder="t('freeChannels.joinDialog.keyNamePlaceholder')" @keydown.enter.prevent="submit" />
      </label>

      <div v-else class="flex items-start gap-3" role="status">
        <span class="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400">
          <Icon name="check" size="sm" />
        </span>
        <div class="min-w-0 space-y-1">
          <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('freeChannels.joinDialog.keyCreated') }}</p>
          <p class="break-words text-xs text-gray-500 dark:text-dark-300">{{ keyName }}</p>
          <p class="text-xs leading-5 text-gray-500 dark:text-dark-300">{{ t('freeChannels.joinDialog.keyCreatedHint') }}</p>
        </div>
      </div>

      <div v-if="credentials.length" class="space-y-4">
        <div v-for="field in credentials" :key="field.id" :data-testid="`shared-${field.id}-field`" class="space-y-2">
          <p :id="`shared-${field.id}-label`" class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ field.label }}</p>
          <div class="flex min-w-0 items-start gap-2 rounded-xl border p-1.5" :class="field.id === 'key' ? 'border-emerald-200/80 bg-emerald-50/40 dark:border-emerald-800/60 dark:bg-emerald-950/20' : 'border-gray-200 bg-gray-50/80 dark:border-dark-700 dark:bg-dark-900/40'">
            <code :data-testid="`shared-${field.id}-value`" :aria-labelledby="`shared-${field.id}-label`" class="min-w-0 flex-1 select-all break-all px-2.5 py-2 font-mono text-xs leading-5 text-gray-700 dark:text-dark-100">{{ field.value }}</code>
            <button
              type="button"
              :data-testid="`copy-shared-${field.id}`"
              :aria-label="field.copyLabel"
              class="inline-flex min-h-9 shrink-0 items-center justify-center gap-1.5 rounded-lg border border-gray-200/80 bg-white px-2.5 text-xs font-medium text-gray-600 shadow-sm transition-colors hover:border-primary-300 hover:text-primary-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200 dark:hover:border-primary-500 dark:hover:text-primary-300"
              @click="copyCredential(field.id, field.value)"
            >
              <Icon :name="clipboards[field.id].copied.value ? 'check' : 'copy'" size="xs" />
              <span aria-live="polite">{{ clipboards[field.id].copied.value ? t('common.copied') : t('common.copy') }}</span>
            </button>
          </div>
          <p v-if="copyErrors[field.id]" role="alert" class="text-xs text-rose-600 dark:text-rose-300">{{ t('common.copyFailed') }}</p>
        </div>
      </div>
      <p v-if="error" role="alert" class="rounded-xl border border-rose-200 bg-rose-50 px-3.5 py-3 text-xs leading-5 text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">{{ error }}</p>
    </div>

    <template #footer>
      <button type="button" class="btn btn-primary min-h-11 w-full !rounded-xl !px-3 text-xs sm:text-sm" :disabled="loading || (!preview && !createdKey && !keyName.trim())" @click="submit">
        <Icon :name="createdKey ? 'check' : loading ? 'refresh' : 'key'" size="sm" class="shrink-0" :class="loading ? 'animate-spin motion-reduce:animate-none' : ''" />
        {{ createdKey ? t('freeChannels.joinDialog.doneAction') : preview ? t('freeChannels.joinDialog.previewAction') : loading ? t('common.loading') : t('freeChannels.joinDialog.createAction') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { useClipboard } from '@/composables/useClipboard'
import type { FreeChannelMock } from './freeChannelsMock'

const props = defineProps<{
  show: boolean
  channel: FreeChannelMock
  apiBaseUrl?: string
  loading?: boolean
  error?: string
  createdKey?: string
  preview?: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'confirmPreview', name: string): void
}>()

type CredentialTarget = 'endpoint' | 'key'
const { t } = useI18n()
const keyName = ref(props.channel.name)
const endpoint = computed(() => props.apiBaseUrl?.trim() ?? '')
const clipboards = { endpoint: useClipboard(), key: useClipboard() }
const copyErrors = reactive({ endpoint: false, key: false })
const credentials = computed(() => [
  { id: 'endpoint' as const, label: t('freeChannels.joinDialog.apiEndpoint'), copyLabel: t('freeChannels.joinDialog.copyEndpoint'), value: endpoint.value },
  { id: 'key' as const, label: 'API Key', copyLabel: t('freeChannels.joinDialog.copyKey'), value: props.createdKey ?? '' },
].filter(field => field.value))

watch(endpoint, () => resetCopyState('endpoint'))
watch(() => props.createdKey, () => resetCopyState('key'))

function resetCopyState(target: CredentialTarget) {
  copyErrors[target] = false
  clipboards[target].copied.value = false
}

async function copyCredential(target: CredentialTarget, value: string) {
  resetCopyState(target)
  copyErrors[target] = !(await clipboards[target].copyToClipboard(value))
}

function submit() {
  if (props.loading) return
  if (props.createdKey) emit('close')
  else if (props.preview) emit('confirmPreview', '')
  else if (keyName.value.trim()) emit('confirmPreview', keyName.value)
}
</script>
