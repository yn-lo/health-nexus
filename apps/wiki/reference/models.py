from django.db import models
from django.utils.translation import gettext_lazy as _


class ArticleReferenceStatus(models.TextChoices):
    PENDING = 'PENDING', _('待审核')
    APPROVED = 'APPROVED', _('已授权')
    REJECTED = 'REJECTED', _('已拒绝')
    INVALIDATED = 'INVALIDATED', _('已失效')
    REVOKED = 'REVOKED', _('已撤销')


class ArticleReference(models.Model):
    source_article = models.ForeignKey(
        'wiki.Article',
        on_delete=models.CASCADE,
        related_name='references_to',
        verbose_name=_("被引用文章")
    )
    target_department = models.ForeignKey(
        'base.Department',
        on_delete=models.CASCADE,
        related_name='article_references',
        verbose_name=_("引用科室")
    )
    status = models.CharField(
        max_length=20,
        choices=ArticleReferenceStatus.choices,
        default=ArticleReferenceStatus.PENDING,
        verbose_name=_("审核状态")
    )
    authorized_by = models.ForeignKey(
        'auth_custom.UserProfile',
        on_delete=models.SET_NULL,
        null=True,
        blank=True,
        verbose_name=_("授权人")
    )
    approved_by_department = models.ForeignKey(
        'base.Department',
        on_delete=models.SET_NULL,
        null=True,
        blank=True,
        related_name='approved_article_references',
        verbose_name=_("审批科室")
    )
    authorized_at = models.DateTimeField(_("授权时间"), null=True, blank=True)
    reason = models.TextField(_("拒绝原因"), blank=True, default='')
    invalidated_at = models.DateTimeField(_("失效时间"), null=True, blank=True, help_text=_("源文章更新时自动设置"))
    invalidated_reason = models.CharField(_("失效原因"), max_length=200, blank=True, default='')
    revoked_by = models.ForeignKey(
        'auth_custom.UserProfile',
        on_delete=models.SET_NULL,
        null=True,
        blank=True,
        related_name='revoked_article_references',
        verbose_name=_("撤销人")
    )
    revoked_at = models.DateTimeField(_("撤销时间"), null=True, blank=True)
    created_at = models.DateTimeField(_("创建时间"), auto_now_add=True)
    updated_at = models.DateTimeField(_("更新时间"), auto_now=True)

    class Meta:
        unique_together = ['source_article', 'target_department']
        verbose_name = "文章引用"
        verbose_name_plural = "文章引用"
        ordering = ['-created_at']

    def __str__(self):
        return f"{self.source_article.title} -> {self.target_department.name} [{self.get_status_display()}]"

    def reconfirm(self, user):
        """重新确认失效的引用，将状态从 INVALIDATED 改为 APPROVED。"""
        if self.status != ArticleReferenceStatus.INVALIDATED:
            return False, "只有已失效的引用才能重新确认"
        
        self.status = ArticleReferenceStatus.APPROVED
        self.authorized_by = user
        self.approved_by_department = self.target_department
        self.authorized_at = models.functions.Now()
        self.invalidated_at = None
        self.invalidated_reason = ''
        self.save(update_fields=['status', 'authorized_by', 'approved_by_department', 'authorized_at', 'invalidated_at', 'invalidated_reason', 'updated_at'])
        return True, "引用已重新确认"

    def revoke(self, user):
        if self.status != ArticleReferenceStatus.APPROVED:
            return False, "只有已授权的引用才能撤销"

        self.status = ArticleReferenceStatus.REVOKED
        self.revoked_by = user
        self.revoked_at = models.functions.Now()
        self.save(update_fields=['status', 'revoked_by', 'revoked_at', 'updated_at'])
        return True, "引用已撤销"
