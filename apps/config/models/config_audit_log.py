from django.db import models
from django.utils.translation import gettext_lazy as _


class ConfigAuditLog(models.Model):
    """
    Configuration change audit log.

    Records all changes to sensitive configuration including:
    - AI provider config (API keys, service URLs)
    - System config (encrypted fields)
    - Other sensitive configuration changes
    """

    class Action(models.TextChoices):
        CREATE = 'CREATE', _('创建')
        UPDATE = 'UPDATE', _('更新')
        DELETE = 'DELETE', _('删除')

    action_type = models.CharField(
        _("操作类型"),
        max_length=10,
        choices=Action.choices,
    )
    config_model = models.CharField(
        _("配置模型"),
        max_length=100,
        help_text=_("被修改的配置模型名称，如 LLMProviderConfig"),
    )
    config_target = models.CharField(
        _("操作目标"),
        max_length=200,
        help_text=_("被修改的配置标识，如配置名称或键名"),
    )
    performed_by = models.CharField(
        _("操作人"),
        max_length=150,
        help_text=_("执行操作的用户名"),
    )
    old_values = models.JSONField(
        _("修改前的值"),
        default=dict,
        blank=True,
        help_text=_("修改前的配置值，敏感字段已掩码"),
    )
    new_values = models.JSONField(
        _("修改后的值"),
        default=dict,
        blank=True,
        help_text=_("修改后的配置值，敏感字段已掩码"),
    )
    created_at = models.DateTimeField(
        _("创建时间"),
        auto_now_add=True,
    )

    class Meta:
        verbose_name = _("配置审计日志")
        verbose_name_plural = verbose_name
        ordering = ['-created_at']

    def __str__(self):
        return f"{self.config_model} - {self.action_type} by {self.performed_by} at {self.created_at}"
