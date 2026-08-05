<template>
  <div class="flex min-h-9 flex-wrap items-center gap-2">
    <slot name="before"></slot>
    <button @click="$emit('refresh')" :disabled="loading" class="btn btn-secondary h-9 px-2.5" :title="t('common.refresh')" :aria-label="t('common.refresh')">
      <Icon name="refresh" size="md" :class="[loading ? 'animate-spin' : '']" />
    </button>
    <slot name="after"></slot>
    <slot name="beforeCreate"></slot>
    <button @click="$emit('create')" class="btn btn-primary h-9 px-3">{{ t('admin.accounts.createAccount') }}</button>
    <slot name="afterCreate"></slot>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps(['loading'])
defineEmits(['refresh', 'create'])

const { t } = useI18n()
</script>
