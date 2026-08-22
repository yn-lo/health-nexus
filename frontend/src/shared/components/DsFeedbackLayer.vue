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
        class="fixed left-1/2 top-1/2 z-[var(--z-toast)] flex -translate-x-1/2 -translate-y-1/2 items-center gap-[var(--spacer-8)] rounded-[var(--radius-8)] bg-black/70 px-[var(--spacer-16)] py-[var(--spacer-10)] text-white shadow-[var(--shadow-lg)] text-body-sm font-medium"
      >
        <span v-if="toastState.type === 'success'" class="ds-toast-icon ds-toast-icon--success" />
        <span v-else-if="toastState.type === 'fail'" class="ds-toast-icon ds-toast-icon--fail">✕</span>
        {{ toastState.message }}
      </div>
    </Transition>

    <Transition name="ds-dialog">
      <div v-if="dialogState.visible" class="ds-dialog-backdrop" @click.self="cancel">
        <div class="ds-dialog">
          <div v-if="dialogState.title" class="ds-dialog__header">{{ dialogState.title }}</div>
          <div class="ds-dialog__body">{{ dialogState.message }}</div>

          <!-- 输入框模式（驳回原因等） -->
          <div v-if="dialogState.showInput" class="ds-dialog__input-wrap">
            <input
              v-model="dialogState.inputValue"
              type="text"
              class="ds-dialog__input"
              :class="{ 'ds-dialog__input--error': dialogState.inputError }"
              :placeholder="dialogState.inputPlaceholder"
            >
            <p v-if="dialogState.inputError" class="ds-dialog__input-error">{{ dialogState.inputError }}</p>
          </div>

          <div class="ds-dialog__footer">
            <button
              v-if="dialogState.showCancel"
              type="button"
              class="ds-dialog__btn ds-dialog__btn--cancel"
              @click="cancel"
            >
              {{ dialogState.cancelButtonText }}
            </button>
            <button
              type="button"
              class="ds-dialog__btn"
              :class="dialogState.isDanger ? 'ds-dialog__btn--danger' : 'ds-dialog__btn--confirm'"
              @click="confirm"
            >
              {{ dialogState.confirmButtonText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>