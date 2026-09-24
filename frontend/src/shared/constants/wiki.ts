/**
 * 知识条目元数据常量 — 与后端 internal/domain/wiki/entity/article.go 对齐
 *
 * 风险等级字面量集中在此处定义，业务代码禁止硬编码 'normal' / 'high'。
 */
import type { ContentRisk } from '../types/wiki'

/** 内容风险等级下拉选项（值对齐后端 ContentRiskNormal / ContentRiskHigh） */
export const CONTENT_RISK_OPTIONS: { value: ContentRisk; label: string }[] = [
  { value: 'normal', label: '普通宣教' },
  { value: 'high', label: '高风险（用药、检查准备、高风险护理）' },
]

/** 内容风险默认值 — 未设置时按普通宣教处理 */
export const CONTENT_RISK_DEFAULT: ContentRisk = 'normal'
