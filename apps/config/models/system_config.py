from django.db import models
from django.utils.translation import gettext_lazy as _


class SystemConfig(models.Model):
    """System configuration (key-value store with categories)"""

    CATEGORIES = [
        ('NETWORK', '网络配置'),
        ('SESSION', '会话管理'),
        ('TASK', '任务队列'),
        ('BRAND', '品牌信息'),
        ('SMS', '短信服务'),
    ]

    DISPLAY_NAMES = {
        'api_timeout': 'API超时时间',
        'max_upload_size': '最大上传大小',
        'cdn_enabled': 'CDN启用开关',
        'session_timeout': '会话超时时间',
        'max_sessions_per_user': '每用户最大会话数',
        'remember_me_days': '记住我天数',
        'max_concurrent_tasks': '最大并发任务数',
        'task_retry_limit': '任务重试次数',
        'task_timeout_seconds': '任务超时时间',
        'task_retry_delay': '任务重试延迟',
        'brand_name': '品牌名称',
        'brand_logo_url': 'Logo地址',
        'brand_contact_email': '联系邮箱',
        'sms_enabled': '短信服务启用',
    }

    category = models.CharField(_("分类"), max_length=30, choices=CATEGORIES)
    config_key = models.CharField(_("配置键"), max_length=100)
    config_value = models.JSONField(_("配置值"))
    is_encrypted = models.BooleanField(_("是否加密"), default=False)
    requires_restart = models.BooleanField(_("需重启生效"), default=False)
    description = models.CharField(_("说明"), max_length=200, blank=True)
    created_at = models.DateTimeField(_("创建时间"), auto_now_add=True)
    updated_at = models.DateTimeField(_("更新时间"), auto_now=True)

    class Meta:
        verbose_name = _("系统配置")
        verbose_name_plural = verbose_name
        unique_together = ['category', 'config_key']

    @property
    def display_name(self):
        """获取配置项的中文显示名称"""
        return self.DISPLAY_NAMES.get(self.config_key, self.config_key)

    def __str__(self):
        return f"[{self.get_category_display()}] {self.display_name} ({self.config_key})"
