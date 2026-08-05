import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export type AccountHealthState = 'unknown' | 'healthy' | 'degraded' | 'recovering' | 'blocked'

export interface AccountHealthSettings {
  enabled: boolean
  auto_assign_group: boolean
  target_group_id: number
  worker_count: number
  batch_size: number
  dispatch_interval_seconds: number
  healthy_interval_seconds: number
  recovery_interval_seconds: number
  failure_threshold: number
  success_threshold: number
  timeout_seconds: number
  model_id: string
  auto_recover: boolean
  auto_block: boolean
}

export interface AccountHealthSummary {
  total: number
  scheduler_eligible: number
  healthy: number
  degraded: number
  recovering: number
  blocked: number
  unknown: number
  in_flight: number
}

export interface AccountHealthItem {
  account_id: number
  name: string
  account_status: string
  schedulable: boolean
  scheduler_eligible: boolean
  state: AccountHealthState
  auto_blocked: boolean
  error_category: string
  error_message: string
  consecutive_successes: number
  consecutive_failures: number
  last_probe_at?: string | null
  last_success_at?: string | null
  next_probe_at?: string | null
  probe_latency_ms: number
  group_names: string[]
}

export interface AccountHealthListParams {
  page?: number
  page_size?: number
  search?: string
  state?: AccountHealthState | ''
  group_id?: number
}

export async function getSummary(groupId?: number): Promise<AccountHealthSummary> {
  const { data } = await apiClient.get<AccountHealthSummary>('/admin/account-health/summary', {
    params: groupId ? { group_id: groupId } : undefined
  })
  return data
}

export async function list(params: AccountHealthListParams): Promise<PaginatedResponse<AccountHealthItem>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountHealthItem>>('/admin/account-health/accounts', { params })
  return data
}

export async function getSettings(): Promise<AccountHealthSettings> {
  const { data } = await apiClient.get<AccountHealthSettings>('/admin/account-health/settings')
  return data
}

export async function updateSettings(settings: AccountHealthSettings): Promise<AccountHealthSettings> {
  const { data } = await apiClient.put<AccountHealthSettings>('/admin/account-health/settings', settings)
  return data
}

export async function runNow(groupId?: number): Promise<{ queued: number }> {
  const { data } = await apiClient.post<{ queued: number }>('/admin/account-health/run', {
    group_id: groupId || 0
  })
  return data
}

export const accountHealthAPI = { getSummary, list, getSettings, updateSettings, runNow }

export default accountHealthAPI
