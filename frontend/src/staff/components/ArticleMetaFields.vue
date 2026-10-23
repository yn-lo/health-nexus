<script setup lang="ts">
import { ChevronDown } from '@lucide/vue'
import { CONTENT_RISK_OPTIONS } from '@/shared/constants/wiki'
import type { ContentRisk } from '@/shared'

const source = defineModel<string>('source', { required: true })
const applicablePopulation = defineModel<string>('applicablePopulation', { required: true })
const validUntil = defineModel<string>('validUntil', { required: true })
const contentRisk = defineModel<ContentRisk>('contentRisk', { required: true })
</script>

<template>
  <div class="flex flex-col gap-[var(--spacer-20)]">
    <!-- 知识来源 -->
    <div class="flex flex-col gap-[var(--spacer-8)]">
      <label class="font-heading text-body-base font-medium text-text">
        知识来源
      </label>
      <div class="ds-field-wrap">
        <input
          v-model="source"
          type="text"
          placeholder="如：《中国高血压防治指南（2024）》，可留空"
        >
      </div>
      <span class="font-heading text-body-xs text-text-tertiary">
        用于复审追溯与检索可见性
      </span>
    </div>

    <!-- 适用人群 -->
    <div class="flex flex-col gap-[var(--spacer-8)]">
      <label class="font-heading text-body-base font-medium text-text">
        适用人群
      </label>
      <div class="ds-field-wrap">
        <input
          v-model="applicablePopulation"
          type="text"
          placeholder="如：高血压患者、孕产妇，可留空"
        >
      </div>
      <span class="font-heading text-body-xs text-text-tertiary">
        用于检索可见性
      </span>
    </div>

    <!-- 有效期至（清空后提交空字符串，表示清空有效期） -->
    <div class="flex flex-col gap-[var(--spacer-8)]">
      <label class="font-heading text-body-base font-medium text-text">
        有效期至
      </label>
      <div class="flex items-center gap-[var(--spacer-8)]">
        <div class="ds-field-wrap flex-1">
          <input
            v-model="validUntil"
            type="date"
            aria-label="有效期至"
          >
        </div>
        <button
          v-if="validUntil"
          type="button"
          class="border-none bg-transparent font-heading text-body-sm text-text-brand"
          @click="validUntil = ''"
        >
          清空
        </button>
      </div>
      <span class="font-heading text-body-xs text-text-tertiary">
        用于复审提醒与检索可见性；到期后进入复审
      </span>
    </div>

    <!-- 内容风险等级 -->
    <div class="flex flex-col gap-[var(--spacer-8)]">
      <label class="font-heading text-body-base font-medium text-text">
        内容风险等级
      </label>
      <div class="relative">
        <select
          v-model="contentRisk"
          class="ds-select"
          aria-label="内容风险等级"
        >
          <option v-for="opt in CONTENT_RISK_OPTIONS" :key="opt.value" :value="opt.value">
            {{ opt.label }}
          </option>
        </select>
        <ChevronDown class="pointer-events-none absolute right-[var(--spacer-12)] top-1/2 h-4 w-4 -translate-y-1/2 text-icon-tertiary" />
      </div>
      <span class="font-heading text-body-xs text-text-tertiary">
        高风险内容（用药、检查准备、高风险护理）逾期后会退出检索
      </span>
    </div>
  </div>
</template>
