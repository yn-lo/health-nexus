import { computed, onMounted, ref } from 'vue'
import { baseApi, getAccessToken } from '@/shared'
import type { Department } from '@/shared'

const ALL_DEPARTMENTS: Department = {
  id: 0,
  name: '全部科室',
  is_public: true,
  parent_id: null,
  is_active: true,
  created_at: '',
}

interface UseDepartmentsOptions {
  /** 初始选中科室 id */
  initialDepartmentId?: number
  /** 是否在 onMounted 自动拉取（默认 true）；设为 false 时需手动调用 fetchDepartments */
  autoFetch?: boolean
  /** 过滤条件：'public' 仅 is_public（默认），'active' 仅 is_active，'all' 不过滤 */
  filter?: 'public' | 'active' | 'all'
  /**
   * 是否在列表首项保留"全部科室"（id=0）。默认 true（用于知识库浏览的科室筛选）。
   * 问答场景必须设 false：问答必须聚焦到某个具体责任科室，不能以"全部科室"进入审查，
   * 否则"统一理解与审查"因无科室语境对常规宣教问题机械追问。
   */
  includeAll?: boolean
}

export function useDepartments(options: UseDepartmentsOptions = {}) {
  const { initialDepartmentId = 0, autoFetch = true, filter = 'public', includeAll = true } = options
  const departments = ref<Department[]>(includeAll ? [ALL_DEPARTMENTS] : [])
  const selectedDepartmentId = ref(initialDepartmentId)

  const activeDepartment = computed(() => {
    return departments.value.find((item) => item.id === selectedDepartmentId.value) ?? departments.value[0]
  })

  async function fetchDepartments() {
    const apply = (data: Department[]) => {
      const filtered = filter === 'public' ? data.filter((d) => d.is_public)
        : filter === 'active' ? data.filter((d) => d.is_active)
        : data
      departments.value = includeAll ? [ALL_DEPARTMENTS, ...filtered] : filtered
      // 无"全部科室"时，选中项必须落到一个具体科室：优先后端标记的默认责任科室（is_default），
      // 否则首项；避免 UI 展示与后端实际锁定科室不一致。
      if (!departments.value.some((d) => d.id === selectedDepartmentId.value)) {
        const fallback = filtered.find((d) => d.is_default) ?? filtered[0]
        selectedDepartmentId.value = fallback ? fallback.id : 0
      }
    }
    try {
      apply(getAccessToken() ? await baseApi.listDepartments() : await baseApi.listPublicDepartments())
    } catch {
      // 登录态失效时 client.ts 已清除 token：降级公开接口，保证匿名仍可选用科室。
      try {
        apply(await baseApi.listPublicDepartments())
      } catch {
        departments.value = [ALL_DEPARTMENTS]
      }
    }
  }

  function selectDepartment(id: number) {
    selectedDepartmentId.value = id
  }

  if (autoFetch) onMounted(fetchDepartments)

  return {
    departments,
    selectedDepartmentId,
    activeDepartment,
    selectDepartment,
    fetchDepartments,
  }
}
