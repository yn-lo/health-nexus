<script setup lang="ts">
/** 免责声明 + 常驻「服务边界」入口：随时可回看知情告知内容（非强制确认模式） */
import { ref } from 'vue'
import ConsentNotice from './ConsentNotice.vue'

const showNotice = ref(false)
</script>

<template>
  <div class="flex flex-col items-center gap-[var(--spacer-4)] mt-[var(--spacer-24)] px-[var(--spacer-16)]">
    <p class="m-0 text-center text-body-sm text-text-tertiary">
      AI 回答仅供参考，不能作为诊断及治疗依据
    </p>
    <button
      type="button"
      class="text-body-sm text-text-brand hover:underline underline-offset-2"
      @click="showNotice = true"
    >
      服务边界与使用须知
    </button>
    <ConsentNotice v-model:show="showNotice" />
  </div>
</template>
