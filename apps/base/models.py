from django.db import models
from django.utils.translation import gettext_lazy as _

class TimeStampedModel(models.Model):
    """抽象基类，提供创建和更新时间"""
    created_at = models.DateTimeField(_("创建时间"), auto_now_add=True)
    updated_at = models.DateTimeField(_("更新时间"), auto_now=True)

    class Meta:
        abstract = True


class Department(TimeStampedModel):
    """
    科室/租户模型
    """
    name = models.CharField(_("科室名称"), max_length=100, unique=True)
    description = models.TextField(_("科室描述"), blank=True)
    is_public = models.BooleanField(_("是否公共空间"), default=False, help_text=_("公共空间的知识对所有科室可见"))
    
    config = models.JSONField(_("科室配置"), default=dict, blank=True, help_text="如：{'welcome_msg': '您好，心内科助手为您服务'}")

    tenant_code = models.CharField(_("租户编码"), max_length=50, blank=True, default='')
    is_active = models.BooleanField(_("是否启用"), default=True, help_text=_("禁用后该科室将不可见"))
    parent = models.ForeignKey(
        'self',
        null=True,
        blank=True,
        on_delete=models.SET_NULL,
        related_name='children',
        verbose_name=_("上级科室")
    )
    manager = models.ForeignKey(
        'auth_custom.UserProfile',
        null=True,
        blank=True,
        on_delete=models.SET_NULL,
        related_name='managed_departments',
        verbose_name=_("科室主任")
    )

    class Meta:
        verbose_name = _("科室")
        verbose_name_plural = _("科室")

    def __str__(self):
        parts = []
        parent = self.parent
        while parent:
            parts.insert(0, parent.name)
            parent = parent.parent
        parts.append(self.name)
        return ' > '.join(parts) if len(parts) > 1 else self.name


class UserDepartment(TimeStampedModel):
    """
    用户-科室多对多关系中间表
    """
    class Role(models.TextChoices):
        ADMIN = 'ADMIN', _('管理员')
        MEMBER = 'MEMBER', _('成员')
        VIEWER = 'VIEWER', _('只读成员')

    department = models.ForeignKey(
        Department,
        on_delete=models.CASCADE,
        related_name='user_departments',
        verbose_name=_("科室")
    )
    user = models.ForeignKey(
        'auth_custom.UserProfile',
        on_delete=models.CASCADE,
        related_name='user_departments',
        verbose_name=_("用户")
    )
    role = models.CharField(_("角色"), max_length=20, choices=Role.choices, default=Role.MEMBER)
    is_primary = models.BooleanField(_("是否主科室"), default=False)

    class Meta:
        verbose_name = _("用户科室关系")
        verbose_name_plural = _("用户科室关系")
        unique_together = ('department', 'user')

    def __str__(self):
        return f"{self.user.username} - {self.department.name} ({self.get_role_display()})"


class ImmutableAuditLogManager(models.Manager):
    """只读审计日志管理器：阻止更新和删除操作"""

    def bulk_update(self, *args, **kwargs):
        raise PermissionError("审计日志不可修改")

    def delete(self):
        raise PermissionError("审计日志不可删除")


class DepartmentAuditLog(TimeStampedModel):
    """
    科室操作审计日志
    """
    class Action(models.TextChoices):
        CREATE = 'create', _('创建')
        UPDATE = 'update', _('更新')
        DELETE = 'delete', _('删除')
        ADD_MEMBER = 'add_member', _('添加成员')
        REMOVE_MEMBER = 'remove_member', _('移除成员')

    department = models.ForeignKey(
        Department,
        on_delete=models.SET_NULL,
        null=True,
        blank=True,
        related_name='audit_logs',
        verbose_name=_("科室")
    )
    performed_by = models.ForeignKey(
        'auth_custom.UserProfile',
        on_delete=models.SET_NULL,
        null=True,
        blank=True,
        verbose_name=_("操作人")
    )
    action_type = models.CharField(_("操作类型"), max_length=50)
    target = models.CharField(_("操作目标"), max_length=200, blank=True, default='')
    details = models.JSONField(_("详细信息"), default=dict, blank=True)

    objects = ImmutableAuditLogManager()

    class Meta:
        verbose_name = _("审计日志")
        verbose_name_plural = _("审计日志")
        ordering = ['-created_at']

    def __str__(self):
        return f"{self.department.name} - {self.action_type} by {self.performed_by}"

    def save(self, *args, **kwargs):
        if self.pk:
            raise PermissionError("审计日志创建后不可修改")
        super().save(*args, **kwargs)

    def delete(self, *args, **kwargs):
        raise PermissionError("审计日志创建后不可删除")


class Notification(TimeStampedModel):
    class Category(models.TextChoices):
        CRISIS = 'CRISIS', _('危机事件')
        REVIEW = 'REVIEW', _('复审提醒')
        SYSTEM = 'SYSTEM', _('系统通知')

    recipient = models.ForeignKey(
        'auth_custom.UserProfile',
        on_delete=models.CASCADE,
        related_name='notifications',
        verbose_name=_("接收人")
    )
    category = models.CharField(_("类别"), max_length=20, choices=Category.choices)
    title = models.CharField(_("标题"), max_length=200)
    content = models.TextField(_("内容"), blank=True, default='')
    is_read = models.BooleanField(_("已读"), default=False, db_index=True)
    related_url = models.URLField(_("相关链接"), max_length=500, blank=True, default='')
    source_id = models.CharField(_("来源ID"), max_length=100, blank=True, default='', help_text=_("关联的业务对象ID"))

    class Meta:
        verbose_name = _("通知")
        verbose_name_plural = _("通知")
        ordering = ['-created_at']
        indexes = [
            models.Index(fields=['recipient', 'is_read'], name='idx_notif_recipient_read'),
        ]

    def __str__(self):
        return f"[{self.get_category_display()}] {self.title} -> {self.recipient.username}"
