from django.contrib import admin
from django.contrib import messages
from unfold.admin import ModelAdmin
from .models import Department, UserDepartment, DepartmentAuditLog
from apps.service_container import container


@admin.register(Department)
class DepartmentAdmin(ModelAdmin):
    list_display = ['name', 'is_public', 'description', 'created_at']
    list_filter = ['is_public']
    search_fields = ['name', 'description']
    ordering = ['name']

    fieldsets = (
        (None, {
            'fields': ('name', 'description', 'is_public'),
            'description': '⚠️ 修改"是否公共"将影响全院知识可见范围，请谨慎操作。'
        }),
        ('层级关系', {
            'fields': ('parent', 'manager'),
            'classes': ('collapse',)
        }),
        ('高级配置', {
            'fields': ('config', 'tenant_code'),
            'classes': ('collapse',),
            'description': '科室独立配置，如欢迎语、专属Prompt等'
        }),
    )

    def save_model(self, request, obj, form, change):
        if change and 'is_public' in form.changed_data:
            old_value = Department.objects.get(pk=obj.pk).is_public
            new_value = obj.is_public
            if old_value != new_value:
                direction = '公共' if new_value else '私有'
                messages.warning(
                    request,
                    f'科室"{obj.name}"已从{"公共" if old_value else "私有"}变更为{direction}，此操作已记录审计日志。'
                )

        service = container.department_service
        if change:
            data = {}
            for field in ['name', 'description', 'is_public', 'tenant_code', 'config']:
                if field in form.changed_data:
                    data[field] = form.cleaned_data[field]
            if 'parent' in form.changed_data:
                data['parent'] = form.cleaned_data['parent']
            if 'manager' in form.changed_data:
                data['manager'] = form.cleaned_data['manager']
            if data:
                dept = service.update_department(obj.pk, data, user=request.user)
                obj.__dict__.update(dept.__dict__)
        else:
            data = {
                'name': form.cleaned_data['name'],
                'tenant_code': form.cleaned_data.get('tenant_code', ''),
                'parent': form.cleaned_data.get('parent'),
                'manager': form.cleaned_data.get('manager'),
                'description': form.cleaned_data.get('description', ''),
                'config': form.cleaned_data.get('config', {}),
            }
            dept = service.create_department(
                data=data,
                user=request.user,
            )
            obj.pk = dept.pk
            obj.__dict__.update(dept.__dict__)

    def delete_model(self, request, obj):
        service = container.department_service
        service.delete_department(obj.pk, user=request.user)


@admin.register(UserDepartment)
class UserDepartmentAdmin(ModelAdmin):
    list_display = ['user', 'department', 'role', 'is_primary', 'created_at']
    list_filter = ['role', 'is_primary', 'department']
    search_fields = ['user__username', 'user__email', 'department__name']
    ordering = ['-created_at']
    list_editable = ['role', 'is_primary']
    autocomplete_fields = ['user', 'department']

    fieldsets = (
        (None, {
            'fields': ('user', 'department', 'role', 'is_primary')
        }),
    )


@admin.register(DepartmentAuditLog)
class DepartmentAuditLogAdmin(ModelAdmin):
    list_display = ['department', 'action_type', 'performed_by', 'target', 'created_at']
    list_filter = ['action_type', 'department', 'created_at']
    search_fields = ['department__name', 'performed_by__username', 'target', 'details']
    ordering = ['-created_at']
    readonly_fields = ['department', 'performed_by', 'action_type', 'target', 'details', 'created_at']

    fieldsets = (
        (None, {
            'fields': ('department', 'performed_by', 'action_type', 'target')
        }),
        ('详细信息', {
            'fields': ('details', 'created_at'),
            'classes': ('collapse',)
        }),
    )
