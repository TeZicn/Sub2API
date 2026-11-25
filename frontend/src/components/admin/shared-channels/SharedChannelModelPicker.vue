<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3">
    <legend class="mb-3 flex w-full items-center justify-between gap-3">
      <span class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.sharedChannels.models') }}</span>
      <span class="text-[10px] tabular-nums text-gray-400">{{ modelValue.length }} / 100</span>
    </legend>

    <div class="rounded-2xl border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-700 dark:bg-dark-900/40">
      <p v-if="!groupId" class="py-3 text-center text-xs text-gray-500">{{ t('admin.sharedChannels.modelsSelectGroup') }}</p>
      <p v-else-if="loading" role="status" class="py-3 text-center text-xs text-gray-500">{{ t('common.loading') }}</p>
      <template v-else>
        <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
          <label class="min-w-0 flex-1 space-y-1.5">
            <span class="text-[10px] font-medium text-gray-500 dark:text-dark-300">{{ t('admin.sharedChannels.modelSource') }}</span>
            <select v-model.number="accountId" data-testid="shared-model-source" class="input text-xs" :disabled="accounts.length === 0">
              <option v-if="!accounts.length" :value="0">{{ t('admin.sharedChannels.noModelSources') }}</option>
              <option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }}</option>
            </select>
          </label>
          <button type="button" data-testid="sync-shared-models" class="btn btn-secondary shrink-0 text-xs" :disabled="!accountId || syncing" @click="syncModels">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': syncing }" />
            {{ t(syncing ? 'admin.accounts.syncUpstreamModelsLoading' : 'admin.accounts.syncUpstreamModels') }}
          </button>
        </div>
        <p v-if="syncMessage" role="status" class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-300">{{ syncMessage }}</p>
        <p v-if="error" role="alert" class="mt-2 text-xs leading-5 text-rose-600 dark:text-rose-300">{{ error }}</p>
        <button v-if="sourcesFailed" type="button" class="mt-2 text-xs font-medium text-primary-600 hover:underline" @click="loadSources">{{ t('common.refresh') }}</button>
      </template>

      <div v-if="options.length" class="mt-3 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-wrap items-center gap-2 border-b border-gray-100 p-2 dark:border-dark-700">
          <input v-model="search" data-testid="shared-model-search" type="search" :aria-label="t('admin.accounts.searchModels')" :placeholder="t('admin.accounts.searchModels')" class="input min-w-0 flex-1 basis-40 text-xs" />
          <button type="button" class="px-1 text-xs font-medium text-primary-600 dark:text-primary-300" :disabled="!filteredOptions.length" @click="selectVisible">{{ t('admin.sharedChannels.selectVisibleModels') }}</button>
        </div>
        <div class="max-h-48 overflow-y-auto overscroll-contain p-1" :aria-label="t('admin.sharedChannels.models')">
          <label v-for="model in filteredOptions" :key="model" class="flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-2 hover:bg-gray-50 dark:hover:bg-dark-700">
            <input type="checkbox" data-testid="shared-model-option" :value="model" :checked="modelValue.includes(model)" :disabled="!modelValue.includes(model) && modelValue.length >= 100" class="h-4 w-4 shrink-0 rounded accent-primary-600" @change="toggleModel(model)" />
            <ModelIcon :model="model" size="16px" />
            <span class="min-w-0 break-all font-mono text-xs leading-5 text-gray-700 dark:text-dark-200">{{ model }}</span>
          </label>
          <p v-if="!filteredOptions.length" class="px-3 py-5 text-center text-xs text-gray-400">{{ t('admin.accounts.noMatchingModels') }}</p>
        </div>
      </div>
      <p v-else-if="accountId && !loading && !error && !syncMessage" class="mt-3 text-xs leading-5 text-gray-400">{{ t('admin.sharedChannels.modelsSyncHint') }}</p>
    </div>

    <div v-if="modelValue.length" class="flex flex-wrap items-center gap-1.5" :aria-label="t('admin.sharedChannels.selectedModels')">
      <button v-for="model in modelValue" :key="model" type="button" class="inline-flex max-w-full items-center gap-1.5 rounded-lg bg-primary-50 px-2 py-1 text-xs text-primary-700 hover:bg-primary-100 dark:bg-primary-950/40 dark:text-primary-300 dark:hover:bg-primary-900/50" :aria-label="t('admin.sharedChannels.removeModel', { model })" @click="toggleModel(model)">
        <span class="truncate">{{ model }}</span><Icon name="x" size="xs" class="shrink-0" />
      </button>
      <button type="button" class="px-1 py-1 text-[10px] text-gray-400 hover:text-rose-600" @click="emit('update:modelValue', [])">{{ t('admin.accounts.clearAllModels') }}</button>
    </div>
    <details class="text-xs text-gray-500 dark:text-dark-300">
      <summary class="w-fit cursor-pointer rounded py-1 focus-visible:outline focus-visible:outline-primary-500">{{ t('admin.accounts.customModelName') }}</summary>
      <ModelTagInput class="mt-2" :models="modelValue" :placeholder="t('admin.accounts.enterCustomModelName')" @update:models="setSelection" />
    </details>
    <p v-if="selectionError" role="alert" class="text-xs text-rose-600 dark:text-rose-300">{{ selectionError }}</p>
    <p class="text-[10px] leading-4 text-gray-400 dark:text-dark-400">{{ t('admin.sharedChannels.modelsHint') }}</p>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as accountsAPI from '@/api/admin/accounts'
import type { SyncUpstreamModelsResult } from '@/api/admin/accounts'
import type { AccountListItem } from '@/types'
import { supportsUpstreamModelSync } from '@/constants/platformCatalog'
import { extractApiErrorMessage } from '@/utils/apiError'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import ModelTagInput from '@/components/admin/channel/ModelTagInput.vue'

const props = defineProps<{ modelValue: string[]; groupId: number; disabled?: boolean; previewModels?: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [models: string[]] }>()
const { t } = useI18n()
const accounts = ref<Pick<AccountListItem, 'id' | 'name' | 'platform'>[]>([])
const accountId = ref(0)
const loading = ref(false)
const syncing = ref(false)
const sourcesFailed = ref(false)
const error = ref('')
const selectionError = ref('')
const syncMessage = ref('')
const candidates = ref<string[]>([])
const search = ref('')
let sourceVersion = 0
let syncVersion = 0

const options = computed(() => [...new Set([...props.modelValue, ...candidates.value])])
const filteredOptions = computed(() => options.value.filter(model => model.toLowerCase().includes(search.value.trim().toLowerCase())))

function resetSync() {
  syncVersion++
  syncing.value = false
  candidates.value = []
  search.value = ''
  error.value = ''
  syncMessage.value = ''
}

watch(accountId, resetSync, { flush: 'sync' })
watch(() => props.groupId, loadSources, { immediate: true })
onUnmounted(() => { sourceVersion++; syncVersion++ })

async function loadSources() {
  const version = ++sourceVersion
  resetSync()
  accounts.value = []
  accountId.value = 0
  sourcesFailed.value = false
  loading.value = false
  if (!props.groupId) return
  loading.value = true
  try {
    if (props.previewModels !== undefined) {
      accounts.value = [{ id: props.groupId, name: t('admin.sharedChannels.previewModelSource'), platform: 'openai' }]
    } else {
      const items: typeof accounts.value = []
      for (let page = 1; ; page++) {
        const result = await accountsAPI.list(page, 100, { group: String(props.groupId), status: 'active', lite: 'true' })
        if (version !== sourceVersion) return
        items.push(...result.items.filter(account => supportsUpstreamModelSync(account.platform)))
        if (page >= result.pages) break
      }
      accounts.value = items
    }
    accountId.value = accounts.value[0]?.id ?? 0
  } catch (cause) {
    if (version !== sourceVersion) return
    sourcesFailed.value = true
    error.value = extractApiErrorMessage(cause, t('admin.sharedChannels.modelSourcesFailed'))
  } finally {
    if (version === sourceVersion) loading.value = false
  }
}

async function syncModels() {
  if (!accountId.value || syncing.value || props.disabled) return
  const version = ++syncVersion
  syncing.value = true
  error.value = ''
  syncMessage.value = ''
  try {
    const result: SyncUpstreamModelsResult = props.previewModels !== undefined
      ? { models: props.previewModels }
      : await accountsAPI.syncUpstreamModels(accountId.value)
    if (version !== syncVersion) return
    if (result.discovery_source === 'configured_fallback') {
      error.value = t('admin.accounts.grokModelDiagnostics.configuredFallback')
      return
    }
    candidates.value = [...new Set(result.models.map(model => model.trim()).filter(Boolean))]
    syncMessage.value = candidates.value.length
      ? t('admin.sharedChannels.modelsSynced', { count: candidates.value.length })
      : t('admin.accounts.syncUpstreamModelsEmpty')
    if (result.warnings?.some(warning => warning.code === 'upstream_model_metadata_incomplete')) {
      syncMessage.value += ` ${t('admin.accounts.syncUpstreamModelsMetadataIncomplete')}`
    } else if (result.warnings?.some(warning => warning.code === 'upstream_model_metadata_partial')) {
      syncMessage.value += ` ${t('admin.accounts.syncUpstreamModelsMetadataPartial')}`
    }
  } catch (cause) {
    if (version !== syncVersion) return
    error.value = extractApiErrorMessage(cause, t('admin.accounts.syncUpstreamModelsFailed'))
  } finally {
    if (version === syncVersion) syncing.value = false
  }
}

function setSelection(models: string[]) {
  selectionError.value = ''
  if (models.length > 100) {
    selectionError.value = t('admin.sharedChannels.modelsLimit')
    return
  }
  emit('update:modelValue', [...models])
}

function toggleModel(model: string) {
  setSelection(props.modelValue.includes(model) ? props.modelValue.filter(value => value !== model) : [...props.modelValue, model])
}

function selectVisible() {
  setSelection([...new Set([...props.modelValue, ...filteredOptions.value])])
}
</script>
