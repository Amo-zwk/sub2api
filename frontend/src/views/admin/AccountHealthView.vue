<template>
  <AppLayout>
    <main class="health-page">
      <header class="health-toolbar">
        <div>
          <div class="flex items-center gap-2.5">
            <span class="live-dot" :class="streamConnected ? 'live-dot-on' : 'live-dot-off'" />
            <span class="text-sm font-medium text-gray-600 dark:text-dark-300">
              {{ summary.in_flight }} {{ t('admin.accountHealth.summary.inFlight') }}
            </span>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="refreshData()">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('admin.accountHealth.actions.refresh') }}
          </button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="running" @click="runChecks">
            <Icon name="play" size="sm" />
            {{ t('admin.accountHealth.actions.run') }}
          </button>
        </div>
      </header>

      <section class="summary-band" aria-label="Account health summary">
        <div v-for="metric in summaryMetrics" :key="metric.key" class="summary-metric">
          <span class="summary-label">{{ metric.label }}</span>
          <strong class="summary-value" :class="metric.color">{{ metric.value }}</strong>
        </div>
      </section>

      <section class="controller-panel">
        <div class="controller-heading">
          <div>
            <h2>{{ t('admin.accountHealth.settings.title') }}</h2>
            <p>{{ t('admin.accountHealth.settings.subtitle') }}</p>
          </div>
          <button type="button" class="btn btn-primary btn-sm" :disabled="saveDisabled" @click="saveSettings">
            <Icon name="sync" size="sm" />
            {{ t('admin.accountHealth.actions.save') }}
          </button>
        </div>

        <div class="toggle-row">
          <label class="toggle-item">
            <Toggle v-model="settings.enabled" />
            <span>{{ t('admin.accountHealth.settings.enabled') }}</span>
          </label>
          <label class="toggle-item">
            <Toggle v-model="settings.auto_recover" />
            <span>{{ t('admin.accountHealth.settings.autoRecover') }}</span>
          </label>
          <label class="toggle-item">
            <Toggle v-model="settings.auto_block" />
            <span>{{ t('admin.accountHealth.settings.autoBlock') }}</span>
          </label>
        </div>

        <div class="auto-group-row">
          <label class="toggle-item auto-group-toggle">
            <Toggle v-model="settings.auto_assign_group" />
            <span>{{ t('admin.accountHealth.settings.autoAssignGroup') }}</span>
          </label>
          <label class="target-group-field">
            <span>{{ t('admin.accountHealth.settings.targetGroup') }}</span>
            <select
              v-model.number="settings.target_group_id"
              class="input"
              :disabled="!settings.auto_assign_group"
            >
              <option :value="0">{{ t('admin.accountHealth.settings.targetGroupPlaceholder') }}</option>
              <option v-for="group in openAIGroups" :key="group.id" :value="group.id">
                {{ group.name }}
              </option>
            </select>
          </label>
        </div>

        <div class="settings-grid">
          <label v-for="field in numericSettings" :key="field.key" class="setting-field">
            <span>{{ field.label }}</span>
            <input v-model.number="settings[field.key]" type="number" :min="field.min" :max="field.max" class="input" />
          </label>
          <label class="setting-field setting-model">
            <span>{{ t('admin.accountHealth.settings.model') }}</span>
            <input v-model.trim="settings.model_id" type="text" class="input" :placeholder="t('admin.accountHealth.settings.modelHint')" />
          </label>
        </div>
      </section>

      <section class="account-panel">
        <div class="account-filters">
          <div class="relative min-w-0 flex-1 sm:max-w-sm">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="search" type="search" class="input w-full pl-10" :placeholder="t('admin.accountHealth.filters.search')" @input="queueSearch" />
          </div>
          <select v-model="stateFilter" class="input state-select" @change="applyFilter">
            <option value="">{{ t('admin.accountHealth.filters.allStates') }}</option>
            <option v-for="state in healthStates" :key="state" :value="state">{{ stateLabel(state) }}</option>
          </select>
          <span class="result-count">{{ pagination.total }}</span>
        </div>

        <div class="hidden overflow-x-auto md:block">
          <table class="health-table">
            <thead>
              <tr>
                <th class="account-col">{{ t('admin.accountHealth.table.account') }}</th>
                <th>{{ t('admin.accountHealth.table.health') }}</th>
                <th>{{ t('admin.accountHealth.table.scheduler') }}</th>
                <th>{{ t('admin.accountHealth.table.failures') }}</th>
                <th class="reason-col">{{ t('admin.accountHealth.table.reason') }}</th>
                <th>{{ t('admin.accountHealth.table.lastProbe') }}</th>
                <th>{{ t('admin.accountHealth.table.nextProbe') }}</th>
                <th class="text-right">{{ t('admin.accountHealth.table.latency') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loading && accounts.length === 0">
                <td colspan="8" class="empty-cell"><span class="loading-spinner" /></td>
              </tr>
              <tr v-else-if="accounts.length === 0">
                <td colspan="8" class="empty-cell">{{ t('common.noData') }}</td>
              </tr>
              <tr v-for="account in accounts" :key="account.account_id">
                <td>
                  <div class="account-name" :title="account.name">{{ account.name }}</div>
                  <div class="account-meta">#{{ account.account_id }}<span v-if="account.group_names?.length"> · {{ account.group_names.join(', ') }}</span></div>
                </td>
                <td><span class="state-badge" :class="stateClass(account.state)">{{ stateLabel(account.state) }}</span></td>
                <td>
                  <span class="scheduler-state" :class="account.scheduler_eligible ? 'scheduler-on' : 'scheduler-off'">
                    <span class="scheduler-dot" />
                    {{ account.scheduler_eligible ? t('admin.accountHealth.table.eligible') : t('admin.accountHealth.table.unavailable') }}
                  </span>
                </td>
                <td class="tabular-nums">{{ account.consecutive_failures }}</td>
                <td>
                  <div class="reason-text" :title="account.error_message || t('admin.accountHealth.table.noError')">
                    <span v-if="account.error_category" class="reason-category">{{ account.error_category }}</span>
                    {{ account.error_message || t('admin.accountHealth.table.noError') }}
                  </div>
                </td>
                <td class="time-cell">{{ formatTime(account.last_probe_at) }}</td>
                <td class="time-cell">{{ formatTime(account.next_probe_at) }}</td>
                <td class="text-right tabular-nums">{{ account.probe_latency_ms ? `${account.probe_latency_ms} ms` : '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="mobile-list md:hidden">
          <article v-for="account in accounts" :key="account.account_id" class="mobile-account">
            <div class="flex min-w-0 items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="account-name">{{ account.name }}</div>
                <div class="account-meta">#{{ account.account_id }}</div>
              </div>
              <span class="state-badge" :class="stateClass(account.state)">{{ stateLabel(account.state) }}</span>
            </div>
            <div class="mobile-details">
              <span>{{ t('admin.accountHealth.table.scheduler') }}</span><strong>{{ account.scheduler_eligible ? t('admin.accountHealth.table.eligible') : t('admin.accountHealth.table.unavailable') }}</strong>
              <span>{{ t('admin.accountHealth.table.failures') }}</span><strong>{{ account.consecutive_failures }}</strong>
              <span>{{ t('admin.accountHealth.table.lastProbe') }}</span><strong>{{ formatTime(account.last_probe_at) }}</strong>
            </div>
            <p class="reason-text mt-3">{{ account.error_message || t('admin.accountHealth.table.noError') }}</p>
          </article>
        </div>

        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.pageSize"
          @update:page="changePage"
          @update:page-size="changePageSize"
        />
      </section>
    </main>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import Toggle from '@/components/common/Toggle.vue'
import { adminAPI } from '@/api/admin'
import { buildApiUrl } from '@/api/client'
import type { AccountHealthItem, AccountHealthSettings, AccountHealthState, AccountHealthSummary } from '@/api/admin/accountHealth'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t, locale } = useI18n()
const appStore = useAppStore()
const accounts = ref<AccountHealthItem[]>([])
const loading = ref(false)
const running = ref(false)
const savingSettings = ref(false)
const streamConnected = ref(false)
const openAIGroups = ref<AdminGroup[]>([])
const search = ref('')
const stateFilter = ref<AccountHealthState | ''>('')
const pagination = reactive({ page: 1, pageSize: 50, total: 0 })
const summary = reactive<AccountHealthSummary>({
  total: 0, scheduler_eligible: 0, healthy: 0, degraded: 0, recovering: 0, blocked: 0, unknown: 0, in_flight: 0
})
const settings = reactive<AccountHealthSettings>({
  enabled: true, auto_assign_group: false, target_group_id: 0,
  worker_count: 64, batch_size: 256, dispatch_interval_seconds: 1,
  healthy_interval_seconds: 120, recovery_interval_seconds: 10, failure_threshold: 3,
  success_threshold: 1, timeout_seconds: 30, model_id: '', auto_recover: true, auto_block: true
})
const healthStates: AccountHealthState[] = ['healthy', 'degraded', 'recovering', 'blocked', 'unknown']

const summaryMetrics = computed(() => [
  { key: 'total', label: t('admin.accountHealth.summary.total'), value: summary.total, color: 'text-gray-900 dark:text-white' },
  { key: 'eligible', label: t('admin.accountHealth.summary.eligible'), value: summary.scheduler_eligible, color: 'text-blue-600 dark:text-blue-400' },
  { key: 'healthy', label: t('admin.accountHealth.summary.healthy'), value: summary.healthy, color: 'text-emerald-600 dark:text-emerald-400' },
  { key: 'degraded', label: t('admin.accountHealth.summary.degraded'), value: summary.degraded, color: 'text-amber-600 dark:text-amber-400' },
  { key: 'recovering', label: t('admin.accountHealth.summary.recovering'), value: summary.recovering, color: 'text-cyan-600 dark:text-cyan-400' },
  { key: 'blocked', label: t('admin.accountHealth.summary.blocked'), value: summary.blocked, color: 'text-red-600 dark:text-red-400' },
  { key: 'unknown', label: t('admin.accountHealth.summary.unknown'), value: summary.unknown, color: 'text-gray-500 dark:text-dark-300' }
])

type NumericSettingKey = Exclude<
  keyof AccountHealthSettings,
  'enabled' | 'auto_assign_group' | 'target_group_id' | 'model_id' | 'auto_recover' | 'auto_block'
>
const numericSettings = computed<Array<{ key: NumericSettingKey; label: string; min: number; max: number }>>(() => [
  { key: 'worker_count', label: t('admin.accountHealth.settings.workers'), min: 1, max: 256 },
  { key: 'batch_size', label: t('admin.accountHealth.settings.batchSize'), min: 1, max: 1000 },
  { key: 'dispatch_interval_seconds', label: t('admin.accountHealth.settings.dispatchInterval'), min: 1, max: 60 },
  { key: 'healthy_interval_seconds', label: t('admin.accountHealth.settings.healthyInterval'), min: 15, max: 86400 },
  { key: 'recovery_interval_seconds', label: t('admin.accountHealth.settings.recoveryInterval'), min: 3, max: 3600 },
  { key: 'failure_threshold', label: t('admin.accountHealth.settings.failureThreshold'), min: 1, max: 20 },
  { key: 'success_threshold', label: t('admin.accountHealth.settings.successThreshold'), min: 1, max: 10 },
  { key: 'timeout_seconds', label: t('admin.accountHealth.settings.timeout'), min: 5, max: 300 }
])
const saveDisabled = computed(() => savingSettings.value || (settings.auto_assign_group && settings.target_group_id <= 0))

let searchTimer: ReturnType<typeof setTimeout> | undefined
let liveRefreshTimer: ReturnType<typeof setTimeout> | undefined
let streamController: AbortController | undefined
let active = true

async function refreshData(showLoading = true) {
  if (showLoading) loading.value = true
  try {
    const [nextSummary, result] = await Promise.all([
      adminAPI.accountHealth.getSummary(),
      adminAPI.accountHealth.list({ page: pagination.page, page_size: pagination.pageSize, search: search.value.trim(), state: stateFilter.value })
    ])
    Object.assign(summary, nextSummary)
    accounts.value = result.items || []
    pagination.total = result.total
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountHealth.messages.loadFailed')))
  } finally {
    if (showLoading) loading.value = false
  }
}

async function loadInitial() {
  loading.value = true
  try {
    const [nextSettings, groups] = await Promise.all([
      adminAPI.accountHealth.getSettings(),
      adminAPI.groups.getAll('openai'),
      refreshData(false)
    ])
    Object.assign(settings, nextSettings)
    openAIGroups.value = groups
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountHealth.messages.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    const updated = await adminAPI.accountHealth.updateSettings({ ...settings })
    Object.assign(settings, updated)
    appStore.showSuccess(t('admin.accountHealth.messages.saveSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    savingSettings.value = false
  }
}

async function runChecks() {
  running.value = true
  try {
    const result = await adminAPI.accountHealth.runNow()
    appStore.showSuccess(t('admin.accountHealth.messages.runQueued', { count: result.queued }))
    scheduleLiveRefresh()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    running.value = false
  }
}

function queueSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(applyFilter, 300)
}

function applyFilter() {
  pagination.page = 1
  void refreshData()
}

function changePage(page: number) {
  pagination.page = page
  void refreshData()
}

function changePageSize(size: number) {
  pagination.pageSize = size
  pagination.page = 1
  void refreshData()
}

function stateLabel(state: AccountHealthState) {
  return t(`admin.accountHealth.states.${state}`)
}

function stateClass(state: AccountHealthState) {
  return `state-${state}`
}

function formatTime(value?: string | null) {
  if (!value) return t('admin.accountHealth.table.never')
  return new Intl.DateTimeFormat(locale.value.startsWith('zh') ? 'zh-CN' : 'en-US', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
  }).format(new Date(value))
}

function scheduleLiveRefresh() {
  if (liveRefreshTimer) return
  liveRefreshTimer = setTimeout(() => {
    liveRefreshTimer = undefined
    void refreshData(false)
  }, 800)
}

async function connectEventStream() {
  while (active) {
    streamController = new AbortController()
    try {
      const token = localStorage.getItem('auth_token')
      const response = await fetch(buildApiUrl('/admin/account-health/events'), {
        credentials: 'include',
        headers: { Accept: 'text/event-stream', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
        signal: streamController.signal
      })
      if (!response.ok || !response.body) throw new Error(`SSE ${response.status}`)
      streamConnected.value = true
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (active) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const frames = buffer.split('\n\n')
        buffer = frames.pop() || ''
        if (frames.some(frame => frame.includes('data:'))) scheduleLiveRefresh()
      }
    } catch (error) {
      if (active && (error as { name?: string }).name !== 'AbortError') streamConnected.value = false
    } finally {
      streamConnected.value = false
    }
    if (active) await new Promise(resolve => setTimeout(resolve, 1500))
  }
}

onMounted(() => {
  void loadInitial()
  void connectEventStream()
})

onBeforeUnmount(() => {
  active = false
  streamController?.abort()
  if (searchTimer) clearTimeout(searchTimer)
  if (liveRefreshTimer) clearTimeout(liveRefreshTimer)
})
</script>

<style scoped>
.health-page { width: 100%; padding: 20px; display: flex; flex-direction: column; gap: 16px; }
.health-toolbar { min-height: 40px; display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.live-dot { width: 8px; height: 8px; border-radius: 999px; flex: none; }
.live-dot-on { background: #10b981; box-shadow: 0 0 0 4px rgb(16 185 129 / 12%); }
.live-dot-off { background: #9ca3af; }
.summary-band { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); border: 1px solid #e5e7eb; border-radius: 6px; background: white; overflow: hidden; }
.summary-metric { min-width: 0; padding: 15px 16px; border-right: 1px solid #e5e7eb; }
.summary-metric:last-child { border-right: 0; }
.summary-label { display: block; color: #6b7280; font-size: 12px; line-height: 1.3; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.summary-value { display: block; margin-top: 5px; font-size: 24px; line-height: 1; font-variant-numeric: tabular-nums; }
.controller-panel, .account-panel { border: 1px solid #e5e7eb; border-radius: 6px; background: white; overflow: hidden; }
.controller-heading { padding: 16px 18px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; border-bottom: 1px solid #e5e7eb; }
.controller-heading h2 { color: #111827; font-size: 16px; line-height: 1.4; font-weight: 650; }
.controller-heading p { margin-top: 2px; color: #6b7280; font-size: 12px; }
.toggle-row { display: flex; flex-wrap: wrap; gap: 28px; padding: 14px 18px; background: #f9fafb; border-bottom: 1px solid #e5e7eb; }
.toggle-item { display: inline-flex; align-items: center; gap: 10px; color: #374151; font-size: 13px; font-weight: 500; }
.auto-group-row { min-height: 66px; padding: 12px 18px; display: flex; align-items: center; gap: 24px; border-bottom: 1px solid #e5e7eb; }
.auto-group-toggle { min-width: 190px; }
.target-group-field { min-width: 0; display: flex; align-items: center; gap: 10px; color: #4b5563; font-size: 12px; font-weight: 500; }
.target-group-field > span { white-space: nowrap; }
.target-group-field .input { width: min(360px, 42vw); }
.settings-grid { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 14px; padding: 16px 18px 18px; }
.setting-field { min-width: 0; }
.setting-field > span { display: block; min-height: 32px; margin-bottom: 5px; color: #4b5563; font-size: 12px; line-height: 16px; }
.setting-model { grid-column: span 4; }
.account-filters { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid #e5e7eb; background: #f9fafb; }
.state-select { width: 190px; }
.result-count { min-width: 42px; height: 34px; display: inline-flex; align-items: center; justify-content: center; border-radius: 5px; background: #e5e7eb; color: #374151; font-size: 12px; font-weight: 650; font-variant-numeric: tabular-nums; }
.health-table { width: 100%; table-layout: fixed; border-collapse: collapse; }
.health-table th { height: 42px; padding: 0 12px; color: #6b7280; background: #fff; border-bottom: 1px solid #e5e7eb; font-size: 12px; font-weight: 600; text-align: left; white-space: nowrap; }
.health-table td { height: 64px; padding: 8px 12px; color: #374151; border-bottom: 1px solid #eef0f3; font-size: 13px; vertical-align: middle; }
.health-table tbody tr:hover { background: #f9fafb; }
.health-table th:nth-child(2) { width: 100px; }
.health-table th:nth-child(3) { width: 110px; }
.health-table th:nth-child(4) { width: 84px; }
.health-table th:nth-child(6), .health-table th:nth-child(7) { width: 132px; }
.health-table th:nth-child(8) { width: 92px; }
.account-col { width: 250px; }
.reason-col { width: auto; }
.account-name { color: #111827; font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.account-meta { margin-top: 3px; color: #9ca3af; font-size: 11px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.state-badge { display: inline-flex; align-items: center; height: 24px; padding: 0 9px; border-radius: 4px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.state-healthy { color: #047857; background: #d1fae5; }
.state-degraded { color: #b45309; background: #fef3c7; }
.state-recovering { color: #0e7490; background: #cffafe; }
.state-blocked { color: #b91c1c; background: #fee2e2; }
.state-unknown { color: #4b5563; background: #e5e7eb; }
.scheduler-state { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 550; white-space: nowrap; }
.scheduler-dot { width: 6px; height: 6px; border-radius: 999px; background: currentColor; }
.scheduler-on { color: #2563eb; }
.scheduler-off { color: #9ca3af; }
.reason-text { max-width: 100%; color: #6b7280; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.reason-category { margin-right: 6px; color: #374151; font-weight: 650; }
.time-cell { color: #6b7280 !important; font-size: 12px !important; font-variant-numeric: tabular-nums; white-space: nowrap; }
.empty-cell { height: 180px !important; color: #9ca3af !important; text-align: center !important; }
.loading-spinner { display: inline-block; width: 22px; height: 22px; border: 2px solid #d1d5db; border-top-color: #0d9488; border-radius: 999px; animation: spin 0.8s linear infinite; }
.mobile-list { padding: 0 14px; }
.mobile-account { padding: 16px 0; border-bottom: 1px solid #e5e7eb; }
.mobile-details { display: grid; grid-template-columns: 1fr auto; gap: 6px 16px; margin-top: 14px; font-size: 12px; }
.mobile-details span { color: #9ca3af; }
.mobile-details strong { color: #374151; font-weight: 600; text-align: right; }
@keyframes spin { to { transform: rotate(360deg); } }
:global(.dark) .summary-band, :global(.dark) .controller-panel, :global(.dark) .account-panel { border-color: #374151; background: #1f2937; }
:global(.dark) .summary-metric { border-color: #374151; }
:global(.dark) .summary-label, :global(.dark) .controller-heading p { color: #9ca3af; }
:global(.dark) .controller-heading, :global(.dark) .toggle-row, :global(.dark) .auto-group-row, :global(.dark) .account-filters { border-color: #374151; }
:global(.dark) .controller-heading h2, :global(.dark) .account-name { color: #f9fafb; }
:global(.dark) .toggle-row, :global(.dark) .account-filters { background: #111827; }
:global(.dark) .toggle-item, :global(.dark) .target-group-field, :global(.dark) .setting-field > span { color: #d1d5db; }
:global(.dark) .health-table th { color: #9ca3af; background: #1f2937; border-color: #374151; }
:global(.dark) .health-table td, :global(.dark) .mobile-account { color: #d1d5db; border-color: #374151; }
:global(.dark) .health-table tbody tr:hover { background: #111827; }
:global(.dark) .result-count { color: #d1d5db; background: #374151; }
:global(.dark) .reason-category { color: #d1d5db; }
@media (max-width: 1279px) {
  .summary-band { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .summary-metric { border-bottom: 1px solid #e5e7eb; }
  .summary-metric:nth-child(4) { border-right: 0; }
  .summary-metric:nth-child(n+5) { border-bottom: 0; }
  .settings-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .setting-model { grid-column: span 4; }
}
@media (max-width: 767px) {
  .health-page { padding: 12px; gap: 12px; }
  .health-toolbar { align-items: flex-start; }
  .summary-band { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .summary-metric, .summary-metric:nth-child(4), .summary-metric:nth-child(n+5) { border-right: 1px solid #e5e7eb; border-bottom: 1px solid #e5e7eb; }
  .summary-metric:nth-child(2n) { border-right: 0; }
  .summary-metric:last-child { border-bottom: 0; }
  .controller-heading { align-items: center; }
  .auto-group-row { align-items: stretch; flex-direction: column; gap: 10px; }
  .auto-group-toggle { min-width: 0; }
  .target-group-field { align-items: stretch; flex-direction: column; gap: 5px; }
  .target-group-field .input { width: 100%; }
  .settings-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .setting-model { grid-column: span 2; }
  .account-filters { flex-wrap: wrap; }
  .state-select { width: calc(100% - 52px); }
}
</style>
