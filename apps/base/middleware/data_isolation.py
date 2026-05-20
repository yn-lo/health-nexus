from django.db.models import Q
from apps.base.models import Department, UserDepartment
from apps.auth.models import UserProfile


class DepartmentDataIsolationMixin:
    """部门数据隔离查询混入类
    
    为 QuerySet 提供基于用户所属科室的数据过滤能力。
    
    过滤规则:
    1. SUPER_ADMIN 角色绕过所有过滤，返回完整数据集
    2. 普通用户仅能看到其直接所属科室的数据
    3. 科室层级仅用于导航，不继承数据权限（BR-BASE-04）
    4. 通过 dept_field 参数指定目标模型上的科室字段名，默认为 'departments'
    """

    def filter_by_user_departments(self, queryset, user, dept_field='departments'):
        """按用户所属科室过滤 QuerySet
        
        Args:
            queryset: 待过滤的 QuerySet
            user: 当前用户
            dept_field: 目标模型上关联 Department 的字段名，
                        支持 ManyToMany 或 ForeignKey，默认 'departments'
        
        Returns:
            过滤后的 QuerySet
        """
        if not user or not user.is_authenticated:
            return queryset.none()

        if self._is_super_admin(user):
            return queryset

        dept_ids = self._get_user_accessible_department_ids(user)
        if not dept_ids:
            return queryset.none()

        filter_key = f'{dept_field}__id__in'
        return queryset.filter(**{filter_key: dept_ids})

    def get_user_accessible_department_ids(self, user):
        """获取用户可访问的科室 ID 列表（公开方法）
        
        仅包含用户直接所属的科室，不包含上级科室。
        """
        return self._get_user_accessible_department_ids(user)

    def _is_super_admin(self, user):
        """判断是否为超级管理员"""
        return (
            hasattr(user, 'role')
            and user.role == UserProfile.Role.SUPER_ADMIN
        )

    def _get_user_accessible_department_ids(self, user):
        """获取用户可访问的科室 ID 集合
        
        仅包括用户通过 UserDepartment 直接关联的科室。
        科室层级仅用于导航，不继承数据权限（BR-BASE-04）。
        """
        user_dept_ids = list(
            UserDepartment.objects.filter(user=user)
            .values_list('department_id', flat=True)
            .distinct()
        )

        return user_dept_ids
