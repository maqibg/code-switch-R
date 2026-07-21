<template>
  <BaseModal :open="open" :title="title" variant="confirm" @close="$emit('cancel')">
    <div class="confirm-content">
      <p>{{ message }}</p>
      <footer>
        <BaseButton variant="outline" :disabled="busy" @click="$emit('cancel')">{{ cancelLabel }}</BaseButton>
        <BaseButton :variant="danger ? 'danger' : 'primary'" :disabled="busy" @click="$emit('confirm')">
          {{ busy ? busyLabel : confirmLabel }}
        </BaseButton>
      </footer>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'

withDefaults(defineProps<{
  open: boolean
  title: string
  message: string
  confirmLabel: string
  cancelLabel?: string
  busyLabel?: string
  danger?: boolean
  busy?: boolean
}>(), {
  cancelLabel: '取消',
  busyLabel: '处理中...',
  danger: false,
  busy: false,
})

defineEmits<{ (event: 'confirm'): void; (event: 'cancel'): void }>()
</script>

<style scoped>
.confirm-content { display: grid; gap: 20px; }
.confirm-content p { margin: 0; color: var(--mac-text-secondary); font-size: .78rem; line-height: 1.65; white-space: pre-line; }
.confirm-content footer { display: flex; justify-content: flex-end; gap: 8px; }
</style>
