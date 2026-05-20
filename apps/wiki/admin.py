from django import forms
from django.contrib import admin, messages
from django.utils.html import format_html
from django.utils.translation import gettext_lazy as _
from unfold.admin import ModelAdmin
from .models import Article, ArticleChunk, ArticleAuditLog
from .reference.models import ArticleReference, ArticleReferenceStatus
from .widgets import LocalQuillFormField
from apps.service_container import container


class ArticleAdminForm(forms.ModelForm):
    class Meta:
        model = Article
        fields = '__all__'

    content = LocalQuillFormField()


def _get_user_departments(user):
    from apps.base.models import UserDepartment
    return UserDepartment.objects.filter(user=user).values_list('department_id', flat=True)


@admin.register(Article)
class ArticleAdmin(ModelAdmin):
    form = ArticleAdminForm
    list_display = [
        'title', 'department', 'status', 'author',
        'view_count', 'review_due_date', 'review_overdue_display',
        'created_at', 'featured_thumbnail',
    ]
    list_filter = ['status', 'department', 'source_type', 'review_overdue', 'created_at']
    search_fields = ['title', 'summary', 'content']
    ordering = ['-created_at']
    prepopulated_fields = {}
    readonly_fields = ['view_count', 'created_at', 'updated_at', 'review_overdue']
    list_select_related = ['department', 'author']
    autocomplete_fields = ['department', 'author']
    actions = ['approve_articles', 'reject_articles', 'archive_articles',
               're_review_approve', 're_review_reject']

    fieldsets = (
        ('基本信息', {
            'fields': ('title', 'summary', 'cover_image', 'department', 'author')
        }),
        ('内容', {
            'fields': ('content', 'source_type')
        }),
        ('状态与审核', {
            'fields': ('status', 'review_due_date', 'review_overdue')
        }),
    )

    def get_queryset(self, request):
        qs = super().get_queryset(request)
        if request.user.is_superuser:
            return qs
        if request.user.role == 'DEPT_ADMIN':
            dept_ids = _get_user_departments(request.user)
            return qs.filter(department_id__in=dept_ids)
        return qs

    def review_overdue_display(self, obj):
        if obj.review_overdue:
            return format_html('<span style="color: red; font-weight: bold;">待复审</span>')
        return '-'
    review_overdue_display.short_description = '复审状态'

    def featured_thumbnail(self, obj):
        if obj.cover_image:
            return format_html(
                '<img src="{}" width="50" height="50" '
                'style="object-fit: cover; border-radius: 4px;" />',
                obj.cover_image.url,
            )
        return "无封面图"
    featured_thumbnail.short_description = '封面图'

    def _check_review_permission(self, request, article):
        if article.author == request.user:
            return False
        if request.user.is_superuser:
            return True
        dept_ids = _get_user_departments(request.user)
        return article.department_id in dept_ids

    @admin.action(description=_('审核通过选中文章'))
    def approve_articles(self, request, queryset):
        wiki_svc = container.wiki_service
        success, fail = 0, 0
        for article in queryset.filter(status=Article.Status.PENDING):
            if not self._check_review_permission(request, article):
                self.message_user(
                    request,
                    _(f'文章"{article.title}"：不可审核自己的文章或无权限审核该科室文章'),
                    messages.ERROR,
                )
                fail += 1
                continue
            try:
                wiki_svc.review_article(article, request.user, 'approve')
                success += 1
            except PermissionError as e:
                self.message_user(request, _(f'文章"{article.title}"：{e}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已审核通过 {success} 篇文章'))

    @admin.action(description=_('审核驳回选中文章'))
    def reject_articles(self, request, queryset):
        wiki_svc = container.wiki_service
        success, fail = 0, 0
        for article in queryset.filter(status=Article.Status.PENDING):
            if not self._check_review_permission(request, article):
                self.message_user(
                    request,
                    _(f'文章"{article.title}"：不可审核自己的文章或无权限审核该科室文章'),
                    messages.ERROR,
                )
                fail += 1
                continue
            try:
                wiki_svc.review_article(article, request.user, 'reject')
                success += 1
            except PermissionError as e:
                self.message_user(request, _(f'文章"{article.title}"：{e}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已驳回 {success} 篇文章'))

    @admin.action(description=_('下线选中文章'))
    def archive_articles(self, request, queryset):
        from apps.wiki.tasks import deactivate_article_chunks
        success, fail = 0, 0
        for article in queryset.filter(status=Article.Status.PUBLISHED):
            can_archive = (
                article.author == request.user
                or request.user.is_superuser
                or request.user.role == 'DEPT_ADMIN'
            )
            if not can_archive:
                self.message_user(
                    request,
                    _(f'文章"{article.title}"：无权限下线该文章'),
                    messages.ERROR,
                )
                fail += 1
                continue
            article.status = Article.Status.ARCHIVED
            article.save(update_fields=['status'])
            deactivate_article_chunks(article)
            ArticleAuditLog.objects.create(
                article=article,
                old_status=Article.Status.PUBLISHED,
                new_status=Article.Status.ARCHIVED,
                changed_by=request.user,
                reason='管理员下线',
            )
            success += 1
        if success:
            self.message_user(request, _(f'已下线 {success} 篇文章'))

    @admin.action(description=_('复审通过选中文章'))
    def re_review_approve(self, request, queryset):
        wiki_svc = container.wiki_service
        success, fail = 0, 0
        for article in queryset.filter(review_overdue=True, status=Article.Status.PUBLISHED):
            if not self._check_review_permission(request, article):
                self.message_user(
                    request,
                    _(f'文章"{article.title}"：无权限复审该科室文章'),
                    messages.ERROR,
                )
                fail += 1
                continue
            try:
                wiki_svc.re_review_article(article, request.user, 'approve')
                success += 1
            except PermissionError as e:
                self.message_user(request, _(f'文章"{article.title}"：{e}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已复审通过 {success} 篇文章'))

    @admin.action(description=_('复审拒绝（下线）选中文章'))
    def re_review_reject(self, request, queryset):
        wiki_svc = container.wiki_service
        success, fail = 0, 0
        for article in queryset.filter(review_overdue=True, status=Article.Status.PUBLISHED):
            if not self._check_review_permission(request, article):
                self.message_user(
                    request,
                    _(f'文章"{article.title}"：无权限复审该科室文章'),
                    messages.ERROR,
                )
                fail += 1
                continue
            try:
                wiki_svc.re_review_article(article, request.user, 'reject')
                success += 1
            except PermissionError as e:
                self.message_user(request, _(f'文章"{article.title}"：{e}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已复审拒绝并下线 {success} 篇文章'))

    def save_model(self, request, obj, form, change):
        wiki_svc = container.wiki_service
        if change:
            original = Article.objects.get(pk=obj.pk)
            old_status = original.status
            new_status = form.cleaned_data.get('status', old_status)

            if old_status in (Article.Status.DRAFT, Article.Status.PUBLISHED):
                obj.status = old_status
                wiki_svc.update_article(
                    article=obj,
                    user=request.user,
                    title=form.cleaned_data.get('title'),
                    content=form.cleaned_data.get('content'),
                    department=form.cleaned_data.get('department'),
                )
            else:
                original.title = form.cleaned_data.get('title', original.title)
                original.content = form.cleaned_data.get('content', original.content)
                original.department = form.cleaned_data.get('department', original.department)
                from apps.wiki.signals import record_article_audit_log
                record_article_audit_log(original, request.user, f"管理员编辑{old_status}文章")
                original.save()

            if new_status != old_status:
                obj.refresh_from_db()
                wiki_svc.change_article_status(obj, request.user, new_status)

            obj.refresh_from_db()
        else:
            article = wiki_svc.create_article(
                title=form.cleaned_data['title'],
                content=form.cleaned_data['content'],
                author=request.user,
                department=form.cleaned_data['department'],
                source_type=form.cleaned_data.get('source_type', 'MANUAL'),
            )
            obj.pk = article.pk
            obj.__dict__.update(article.__dict__)

    def has_delete_permission(self, request, obj=None):
        if obj is None:
            return True
        if request.user.is_superuser:
            return True
        if obj.author == request.user and obj.status in (
            Article.Status.DRAFT, Article.Status.PENDING,
        ):
            return True
        return False


@admin.register(ArticleChunk)
class ArticleChunkAdmin(ModelAdmin):
    list_display = ['article', 'department', 'chunk_index', 'content_preview']
    list_filter = ['department']
    search_fields = ['article__title', 'content_text']
    ordering = ['article', 'chunk_index']
    readonly_fields = ['embedding_display', 'department']
    exclude = ['embedding']

    def has_add_permission(self, request):
        return False

    def has_change_permission(self, request, obj=None):
        return False

    def has_delete_permission(self, request, obj=None):
        return False

    def embedding_display(self, obj):
        if obj.embedding is None:
            return '-'
        return f"向量维度: {len(obj.embedding) if hasattr(obj.embedding, '__len__') else 'N/A'}"
    embedding_display.short_description = '向量数据'

    def content_preview(self, obj):
        return obj.content_text[:100] + "..." if len(obj.content_text) > 100 else obj.content_text
    content_preview.short_description = '内容预览'


@admin.register(ArticleAuditLog)
class ArticleAuditLogAdmin(ModelAdmin):
    list_display = ['article', 'old_status', 'new_status', 'changed_by', 'reason', 'created_at']
    list_filter = ['new_status', 'created_at']
    search_fields = ['article__title', 'changed_by__username', 'reason']
    ordering = ['-created_at']
    readonly_fields = ['article', 'old_status', 'new_status', 'changed_by', 'reason', 'change_summary', 'created_at']
    list_select_related = ['article', 'changed_by']

    fieldsets = (
        (None, {'fields': ('article', 'old_status', 'new_status', 'changed_by')}),
        ('详细信息', {'fields': ('reason', 'change_summary', 'created_at'), 'classes': ('collapse',)}),
    )

    def has_add_permission(self, request):
        return False

    def has_change_permission(self, request, obj=None):
        return False

    def has_delete_permission(self, request, obj=None):
        return False


@admin.register(ArticleReference)
class ArticleReferenceAdmin(ModelAdmin):
    list_display = [
        'source_article', 'target_department', 'status',
        'authorized_by', 'authorized_at', 'created_at',
    ]
    list_filter = ['status', 'target_department']
    search_fields = ['source_article__title', 'target_department__name']
    ordering = ['-created_at']
    readonly_fields = ['created_at', 'updated_at']
    autocomplete_fields = ['source_article', 'target_department', 'authorized_by']
    actions = ['approve_references', 'reject_references', 'revoke_references']

    fieldsets = (
        (None, {
            'fields': ('source_article', 'target_department', 'status'),
        }),
        ('授权信息', {
            'fields': ('authorized_by', 'approved_by_department', 'authorized_at'),
            'classes': ('collapse',),
        }),
        ('拒绝/失效/撤销', {
            'fields': ('reason', 'invalidated_at', 'invalidated_reason', 'revoked_by', 'revoked_at'),
            'classes': ('collapse',),
        }),
    )

    def get_queryset(self, request):
        qs = super().get_queryset(request)
        if request.user.is_superuser:
            return qs
        if request.user.role == 'DEPT_ADMIN':
            dept_ids = _get_user_departments(request.user)
            return qs.filter(
                source_article__department_id__in=dept_ids,
            )
        return qs

    @admin.action(description=_('批准选中引用'))
    def approve_references(self, request, queryset):
        ref_svc = container.article_reference_service
        success, fail = 0, 0
        for ref in queryset.filter(status=ArticleReferenceStatus.PENDING):
            ok, msg = ref_svc.approve_reference(
                ref.source_article_id, ref.target_department_id, request.user,
            )
            if ok:
                success += 1
            else:
                self.message_user(request, _(f'引用 {ref}：{msg}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已批准 {success} 条引用'))

    @admin.action(description=_('拒绝选中引用'))
    def reject_references(self, request, queryset):
        ref_svc = container.article_reference_service
        success, fail = 0, 0
        for ref in queryset.filter(status=ArticleReferenceStatus.PENDING):
            ok, msg = ref_svc.reject_reference(
                ref.source_article_id, ref.target_department_id,
                reason='管理员拒绝', user=request.user,
            )
            if ok:
                success += 1
            else:
                self.message_user(request, _(f'引用 {ref}：{msg}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已拒绝 {success} 条引用'))

    @admin.action(description=_('撤销选中引用'))
    def revoke_references(self, request, queryset):
        ref_svc = container.article_reference_service
        success, fail = 0, 0
        for ref in queryset.filter(status=ArticleReferenceStatus.APPROVED):
            ok, msg = ref_svc.revoke_reference(
                ref.source_article_id, ref.target_department_id, request.user,
            )
            if ok:
                success += 1
            else:
                self.message_user(request, _(f'引用 {ref}：{msg}'), messages.ERROR)
                fail += 1
        if success:
            self.message_user(request, _(f'已撤销 {success} 条引用'))
