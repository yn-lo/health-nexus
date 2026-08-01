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
}

export function useDepartments(options: UseDepartmentsOptions = {}) {
  const { initialDepartmentId = 0, autoFetch = true, filter = 'public' } = options
  const departments = ref<Department[]>([ALL_DEPARTMENTS])
  const selectedDepartmentId = ref(initialDepartmentId)

  const activeDepartment = computed(() => {
    return departments.value.find((item) => item.id === selectedDepartmentId.value) ?? departments.value[0]
  })

  async function fetchDepartments() {
    try {
      const data = getAccessToken()
        ? await baseApi.listDepartments()
        : await baseApi.listPublicDepartments()
      const filtered = filter === 'public' ? data.filter((d) => d.is_public)
        : filter === 'active' ? data.filter((d) => d.is_active)
        : data
      departments.value = [ALL_DEPARTMENTS, ...filtered]
    } catch {
      departments.value = [ALL_DEPARTMENTS]
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
