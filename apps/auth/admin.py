from django.contrib import admin, messages
from django.shortcuts import render, redirect
from django.urls import path
from django.http import HttpResponse
from django.contrib.admin.views.decorators import staff_member_required
from django.template.response import TemplateResponse
from unfold.admin import ModelAdmin, StackedInline
from django.contrib.auth.admin import UserAdmin as BaseUserAdmin
from .models import UserProfile
from apps.service_container import container
from apps.auth.bind.models import PatientDoctorBinding
from apps.base.models import UserDepartment
import openpyxl


def _bulk_import_download_template(request):
    wb = openpyxl.Workbook()
    ws = wb.active
    ws.title = "用户导入模板"

    headers = ['姓名', '手机号', '科室', '性别', '角色', '出生日期']
    ws.append(headers)

    ws.append(['李医生', '13800138001', '心内科', 'M', '医生', ''])
    ws.append(['王护士', '13800138002', '骨科', 'F', '护士', ''])
    ws.append(['张三', '13800138003', '心内科', 'M', '患者', '1990-01-01'])

    for col_idx in range(1, len(headers) + 1):
        ws.cell(row=1, column=col_idx).font = openpyxl.styles.Font(bold=True)

    response = HttpResponse(
        content_type='application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    )
    response['Content-Disposition'] = 'attachment; filename="用户导入模板.xlsx"'
    wb.save(response)
    return response


def _bulk_import_view(request):
    from django.contrib import admin

    if request.method == 'POST':
        file = request.FILES.get('excel_file')
        if not file:
            messages.error(request, '请上传Excel文件')
            return redirect('admin:auth_custom_userprofile_bulk_import')

        try:
            service = container.bulk_import_service
            result = service.import_from_excel(file, request.user)

            if result.success_count > 0:
                messages.success(request, f'成功导入 {result.success_count} 条记录')
            if result.fail_count > 0:
                error_summary = '; '.join(result.errors[:5])
                if len(result.errors) > 5:
                    error_summary += f'... 还有 {len(result.errors) - 5} 个错误'
                messages.error(request, f'失败 {result.fail_count} 条: {error_summary}')

        except Exception as e:
            messages.error(request, f'导入失败: {str(e)}')

        return redirect('admin:auth_custom_userprofile_changelist')

    admin_site = admin.site
    context = admin_site.each_context(request)
    context['title'] = '批量导入用户'
    return render(request, 'admin/bulk_import.html', context)


bulk_import_view = staff_member_required(_bulk_import_view)
bulk_import_download_template = staff_member_required(_bulk_import_download_template)


class UserDepartmentInline(StackedInline):
    model = UserDepartment
    extra = 1
    autocomplete_fields = ['department']


@admin.register(UserProfile)
class UserProfileAdmin(BaseUserAdmin, ModelAdmin):
    list_display = ['username', 'email', 'role', 'agreed_terms', 'is_active', 'date_joined']
    list_filter = ['role', 'is_active', 'agreed_terms']
    search_fields = ['username', 'email', 'first_name', 'last_name', 'phone']
    ordering = ['-date_joined']
    list_editable = ['role']
    change_list_template = 'admin/auth_custom/userprofile/change_list.html'
    inlines = [UserDepartmentInline]

    fieldsets = BaseUserAdmin.fieldsets + (
        ('扩展信息', {'fields': ('role', 'phone', 'avatar', 'agreed_terms', 'agreed_terms_at')}),
    )

    readonly_fields = ['agreed_terms_at']

    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path('bulk_import/', bulk_import_view, name='auth_custom_userprofile_bulk_import'),
            path('bulk_import/download_template/', bulk_import_download_template, name='auth_custom_userprofile_bulk_import_template'),
        ]
        return custom_urls + urls


    def bulk_import_action(self, request, queryset):
        pass
    bulk_import_action.short_description = '批量导入用户'


@admin.register(PatientDoctorBinding)
class PatientDoctorBindingAdmin(ModelAdmin):
    list_display = ['doctor', 'patient', 'initiator', 'status', 'requested_at', 'confirmed_at']
    list_filter = ['status', 'initiator']
    search_fields = ['doctor__username', 'patient__username']
    ordering = ['-requested_at']
    autocomplete_fields = ['doctor', 'patient']
    readonly_fields = ['requested_at', 'confirmed_at', 'initiator', 'status']
