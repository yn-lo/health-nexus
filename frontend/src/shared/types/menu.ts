import type { Component } from 'vue'

/** 菜单项（PersonalCenter / StaffProfile 共用） */
export interface MenuItem {
  icon: Component
  label: string
  routeName: string
  value?: string
  danger?: boolean
}
