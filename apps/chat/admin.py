from django.contrib import admin, messages
from django.utils.translation import gettext_lazy as _
from django.shortcuts import render
from django.urls import path
from django.contrib.admin.views.decorators import staff_member_required
from unfold.admin import ModelAdmin
from .models import Conversation, Message, PromptTemplate, PromptTemplateType, HotQuestion
from apps.service_container import container


@admin.register(Conversation)
class ConversationAdmin(ModelAdmin):
    list_display = ['patient', 'title', 'is_archived', 'message_count', 'created_at', 'updated_at']
    list_filter = ['is_archived', 'created_at', 'updated_at']
    search_fields = ['patient__user__username', 'patient__name', 'title']
    ordering = ['-updated_at']
    readonly_fields = ['id', 'created_at', 'updated_at']
    list_select_related = ['patient', 'patient__user']
    actions = ['archive_conversations', 'unarchive_conversations']

    def message_count(self, obj):
        return obj.messages.count()
    message_count.short_description = '消息数量'

    @admin.action(description=_("归档会话"))
    def archive_conversations(self, request, queryset):
        from django.utils import timezone
        updated = queryset.filter(is_archived=False).update(
            is_archived=True, archived_at=timezone.now()
        )
        messages.success(request, _(f"已归档 {updated} 个会话"))

    @admin.action(description=_("取消归档"))
    def unarchive_conversations(self, request, queryset):
        updated = queryset.filter(is_archived=True).update(
            is_archived=False, archived_at=None
        )
        messages.success(request, _(f"已取消归档 {updated} 个会话"))

    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path('stats_dashboard/', self.stats_dashboard_view, name='chat_stats_dashboard'),
        ]
        return custom_urls + urls

    @staff_member_required
    def stats_dashboard_view(self, request):
        from apps.base.models import Department

        stats_svc = container.stats_service

        departments = Department.objects.exclude(is_public=True)
        dashboard_data = []

        for dept in departments:
            coverage = stats_svc.get_knowledge_coverage(department_id=dept.id)
            feedback = stats_svc.get_feedback_stats(department_id=dept.id)
            dashboard_data.append({
                'department': dept.name,
                'total_questions': coverage['total_questions'],
                'coverage_rate': coverage['coverage_rate'],
                'satisfaction_rate': feedback['satisfaction_rate'],
                'disliked_count': feedback['negative_feedbacks'],
            })

        global_coverage = stats_svc.get_knowledge_coverage()
        global_feedback = stats_svc.get_feedback_stats()

        context = {
            'title': _('数据统计看板'),
            'dashboard_data': dashboard_data,
            'global_stats': {
                'total_questions': global_coverage['total_questions'],
                'coverage_rate': global_coverage['coverage_rate'],
                'satisfaction_rate': global_feedback['satisfaction_rate'],
                'sensitive_interceptions': global_coverage['intercepted_questions'],
            },
        }
        return render(request, 'admin/stats_dashboard.html', context)


class DislikedMessageQueueAdmin(admin.ModelAdmin):
    """差评待优化队列视图，同时作为Message的Admin"""
    list_display = ['conversation', 'sender', 'feedback_display', 'review_status_display', 'content_preview', 'created_at']
    list_filter = ['sender', 'feedback', 'review_status', 'is_safety_flagged', 'created_at']
    search_fields = ['conversation__title', 'content']
    ordering = ['-created_at']
    readonly_fields = ['created_at', 'updated_at']
    actions = ['mark_as_reviewed', 'restore_intercepted_content', 'mark_correction_needed']
    autocomplete_fields = ['conversation']

    def content_preview(self, obj):
        content = obj.content[:100] + "..." if len(obj.content) > 100 else obj.content
        return content
    content_preview.short_description = '内容预览'

    def feedback_display(self, obj):
        feedback_choices = {
            0: '未评价',
            1: '有用',
            -1: '无用/错误'
        }
        return feedback_choices.get(obj.feedback, '未知')
    feedback_display.short_description = '用户评价'

    def review_status_display(self, obj):
        status_map = {0: '待处理', 1: '已处理'}
        return status_map.get(obj.review_status, '未知')
    review_status_display.short_description = '处理状态'

    @admin.action(description=_("标记为已审核"))
    def mark_as_reviewed(self, request, queryset):
        from django.utils import timezone

        pending_messages = queryset.filter(
            review_status=Message.ReviewStatus.PENDING
        )
        updated_count = pending_messages.count()

        pending_messages.update(
            review_status=Message.ReviewStatus.REVIEWED,
            reviewed_by=request.user,
            reviewed_at=timezone.now()
        )
        messages.success(request, _(f"已审核 {updated_count} 条差评消息"))

    @admin.action(description=_("恢复误拦截内容"))
    def restore_intercepted_content(self, request, queryset):
        from django.utils import timezone

        intercepted = queryset.filter(
            is_safety_flagged=True,
            original_content__gt=''
        )
        restored_count = 0
        for msg in intercepted:
            msg.content = msg.original_content
            msg.original_content = ''
            msg.is_safety_flagged = False
            msg.safety_flag_level = ''
            msg.safety_flag_reason = 'RESTORED:' + msg.safety_flag_reason
            msg.save(update_fields=['content', 'original_content', 'is_safety_flagged', 'safety_flag_level', 'safety_flag_reason'])
            restored_count += 1

        messages.success(request, _(f"已恢复 {restored_count} 条误拦截内容"))

    @admin.action(description=_("标记需要知识库修正"))
    def mark_correction_needed(self, request, queryset):
        from django.utils import timezone

        disliked = queryset.filter(
            feedback=Message.Feedback.DISLIKE,
            review_status=Message.ReviewStatus.PENDING,
        )
        updated_count = disliked.count()
        disliked.update(
            review_status=Message.ReviewStatus.CORRECTION_NEEDED,
            correction_status=Message.CorrectionStatus.PENDING,
            reviewed_by=request.user,
            reviewed_at=timezone.now(),
        )

        from apps.base.services import NotificationService
        from apps.base.models import Notification
        notif_svc = NotificationService()
        for msg in disliked:
            dept_ids = msg.conversation.departments.values_list('id', flat=True) if msg.conversation.departments.exists() else []
            if dept_ids:
                from apps.base.models import Department
                for dept in Department.objects.filter(id__in=dept_ids):
                    notif_svc.notify_department_staff(
                        department=dept,
                        category=Notification.Category.SYSTEM,
                        title="知识库修正任务",
                        content=f"差评消息（ID:{msg.id}）确认需要知识库修正，请及时处理。",
                        source_id=str(msg.id),
                    )

        messages.success(request, _(f"已标记 {updated_count} 条消息需要知识库修正"))

    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path('disliked_queue/', self.disliked_queue_view, name='disliked_queue'),
        ]
        return custom_urls + urls

    @staff_member_required
    def disliked_queue_view(self, request):
        from apps.base.models import Department

        department_id = request.GET.get('department_id')
        feedback_svc = container.feedback_service
        disliked_messages = feedback_svc.get_disliked_messages_with_details(
            department_id=department_id,
            limit=100
        )
        departments = Department.objects.exclude(is_public=True)

        context = {
            'title': _('待优化队列'),
            'disliked_messages': disliked_messages,
            'departments': departments,
            'selected_department_id': department_id,
            'opts': Message._meta,
        }
        return render(request, 'admin/disliked_queue.html', context)


admin.site.register(Message, DislikedMessageQueueAdmin)


@admin.register(PromptTemplate)
class PromptTemplateAdmin(ModelAdmin):
    list_display = ['type_display', 'is_active', 'content_preview', 'updated_at']
    list_filter = ['type', 'is_active']
    search_fields = ['type', 'content']
    ordering = ['type']
    readonly_fields = ['created_at', 'updated_at']

    def type_display(self, obj):
        return obj.get_type_display()
    type_display.short_description = _('模板类型')

    def content_preview(self, obj):
        return obj.content[:80] + "..." if len(obj.content) > 80 else obj.content
    content_preview.short_description = _('内容预览')

    def has_add_permission(self, request):
        return request.user.is_superuser

    def has_change_permission(self, request, obj=None):
        return request.user.is_superuser

    def has_delete_permission(self, request, obj=None):
        return request.user.is_superuser


@admin.register(HotQuestion)
class HotQuestionAdmin(ModelAdmin):
    list_display = ['question', 'department', 'sort_order', 'is_active', 'created_at']
    list_filter = ['is_active', 'department']
    search_fields = ['question']
    ordering = ['sort_order', 'id']
    list_editable = ['sort_order', 'is_active']
    autocomplete_fields = ['department']

    fieldsets = (
        (None, {
            'fields': ('question', 'department', 'is_active')
        }),
        ('排序', {
            'fields': ('sort_order',),
            'description': '数字越小越靠前，相同排序时按创建时间排序'
        }),
    )
