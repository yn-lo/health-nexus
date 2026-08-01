<script setup lang="ts">
import { useDsToast, useDsDialog } from '@/shared/composables'

const { toastState } = useDsToast()
const { dialogState, confirm, cancel } = useDsDialog()
</script>

<template>
  <Teleport to="body">
    <Transition name="ds-toast">
      <div
        v-if="toastState.visible"
        class="fixed top-[var(--spacer-16)] left-1/2 -translate-x-1/2 z-[var(--z-toast)] flex items-center gap-[var(--spacer-8)] rounded-[var(--radius-8)] px-[var(--spacer-16)] py-[var(--spacer-10)] shadow-[var(--shadow-lg)] text-body-sm font-medium"
        :class="toastState.type === 'success'
          ? 'bg-[var(--status-success-surface-l1)] text-[var(--status-success-default)]'
          : 'bg-[var(--status-error-surface-l1)] text-[var(--status-error-default)]'"
      >
        <span v-if="toastState.type === 'success'" class="ds-toast-icon ds-toast-icon--success" />
        <span v-else class="ds-toast-icon ds-toast-icon--fail">✕</span>
        {{ toastState.message }}
      </div>
    </Transition>

    <Transition name="ds-dialog">
      <div v-if="dialogState.visible" class="ds-dialog-backdrop" @click.self="cancel">
        <div class="ds-dialog">
          <div v-if="dialogState.title" class="ds-dialog__header">{{ dialogState.title }}</div>
          <div class="ds-dialog__body">{{ dialogState.message }}</div>
          <div class="ds-dialog__footer">
            <button type="button" class="ds-dialog__btn ds-dialog__btn--cancel" @click="cancel">{{ dialogState.cancelButtonText }}</button>
            <button type="button" class="ds-dialog__btn" :class="dialogState.isDanger ? 'ds-dialog__btn--danger' : 'ds-dialog__btn--confirm'" @click="confirm">{{ dialogState.confirmButtonText }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
