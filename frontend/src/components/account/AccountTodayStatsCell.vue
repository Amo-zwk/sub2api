<template>
  <div class="min-w-0" data-testid="account-today-stats">
    <!-- Loading state -->
    <div v-if="props.loading && !props.stats" class="grid grid-cols-2 gap-x-3 gap-y-1.5">
      <div v-for="index in 4" :key="index" class="space-y-1">
        <div class="h-2.5 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-3 w-14 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
      </div>
    </div>

    <!-- Error state -->
    <div v-else-if="props.error && !props.stats" class="text-xs text-red-500">
      {{ props.error }}
    </div>

    <!-- Stats data -->
    <div v-else-if="props.stats" class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
      <!-- Requests -->
      <div class="flex min-w-0 flex-col leading-4">
        <span class="text-[11px] text-gray-500 dark:text-gray-400"
          >{{ t('admin.accounts.stats.requests') }}:</span
        >
        <span class="truncate font-medium tabular-nums text-gray-700 dark:text-gray-300">{{
          formatNumber(props.stats.requests)
        }}</span>
      </div>
      <!-- Tokens -->
      <div class="flex min-w-0 flex-col leading-4">
        <span class="text-[11px] text-gray-500 dark:text-gray-400"
          >{{ t('admin.accounts.stats.tokens') }}:</span
        >
        <span class="truncate font-medium tabular-nums text-gray-700 dark:text-gray-300">{{
          formatTokens(props.stats.tokens)
        }}</span>
      </div>
      <!-- Cost (Account) -->
      <div class="flex min-w-0 flex-col leading-4">
        <span class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('usage.accountBilled') }}:</span>
        <span class="truncate font-medium tabular-nums text-emerald-600 dark:text-emerald-400">{{
          formatCurrency(props.stats.cost)
        }}</span>
      </div>
      <!-- Cost (User/API Key) -->
      <div v-if="props.stats.user_cost != null" class="flex min-w-0 flex-col leading-4">
        <span class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('usage.userBilled') }}:</span>
        <span class="truncate font-medium tabular-nums text-gray-700 dark:text-gray-300">{{
          formatCurrency(props.stats.user_cost)
        }}</span>
      </div>
    </div>

    <!-- No data -->
    <div v-else class="text-xs text-gray-400">-</div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { WindowStats } from '@/types'
import { formatNumber, formatCurrency } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    stats?: WindowStats | null
    loading?: boolean
    error?: string | null
  }>(),
  {
    stats: null,
    loading: false,
    error: null
  }
)

const { t } = useI18n()

// Format large token numbers (e.g., 1234567 -> 1.23M)
const formatTokens = (tokens: number): string => {
  if (tokens >= 1000000) {
    return `${(tokens / 1000000).toFixed(2)}M`
  } else if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}K`
  }
  return tokens.toString()
}
</script>
