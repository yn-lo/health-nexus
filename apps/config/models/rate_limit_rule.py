from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.config.models import BaseConfig
import logging

logger = logging.getLogger(__name__)


class RateLimitRule(BaseConfig):

    RULE_TYPES = [
        ('GLOBAL', '全局限流'),
        ('PATH', '路径限流'),
        ('SMS', '短信限流'),
        ('ANONYMOUS_CHAT', '匿名聊天限流'),
    ]

    GLOBAL_RULE_NAMES = {
        'anonymous_global': '匿名用户',
        'authenticated_global': '登录用户',
        'anonymous_chat': '匿名聊天',
    }

    PATH_RULE_NAMES = {
        'login': '登录接口',
        'staff_login': '医护登录接口',
        'patient_login': '患者登录接口',
        'phone_login': '手机验证码登录接口',
        'password_reset': '密码重置接口',
        'register': '注册接口',
        'chat_send': '聊天发送',
        'chat_streaming': '聊天流式发送',
        'chat_access': '聊天页面访问',
    }

    SMS_RULE_NAMES = {
        'ip': '按IP',
        'phone': '按手机号',
        'attempts': '发送次数',
    }

    ANONYMOUS_CHAT_RULE_NAMES = {
        'daily': '每日次数',
    }

    name = models.CharField(
        _("规则标识"),
        max_length=50,
        unique=True,
        help_text=_("规则的唯一英文标识，如 anonymous、authenticated、login 等"),
    )
    rule_type = models.CharField(_("规则类型"), max_length=20, choices=RULE_TYPES)
    path = models.CharField(_("路径"), max_length=200, blank=True, null=True)
    methods = models.JSONField(_("请求方法"), default=list)
    limit = models.PositiveIntegerField(_("限制次数"))
    window = models.PositiveIntegerField(_("时间窗口(秒)"))
    description = models.CharField(_("说明"), max_length=200, blank=True)

    @property
    def display_name(self):
        """获取规则的中文显示名称"""
        if self.rule_type == 'GLOBAL':
            return self.GLOBAL_RULE_NAMES.get(self.name, self.name)
        elif self.rule_type == 'PATH':
            return self.PATH_RULE_NAMES.get(self.name, self.name)
        elif self.rule_type == 'SMS':
            return self.SMS_RULE_NAMES.get(self.name, self.name)
        elif self.rule_type == 'ANONYMOUS_CHAT':
            return self.ANONYMOUS_CHAT_RULE_NAMES.get(self.name, self.name)
        return self.name

    class Meta:
        verbose_name = _("限流规则")
        verbose_name_plural = verbose_name
        ordering = ['rule_type', 'name']

    def __str__(self):
        return f"{self.get_rule_type_display()} - {self.display_name}"

    GLOBAL_NAME_TO_USER_TYPE = {
        'anonymous_global': 'anonymous',
        'authenticated_global': 'authenticated',
    }

    def invalidate_cache(self):
        from django.core.cache import cache
        from apps.config.services.config_service import ConfigService

        if self.rule_type == 'GLOBAL':
            user_type = self.GLOBAL_NAME_TO_USER_TYPE.get(self.name)
            if user_type:
                ConfigService.delete(f"ratelimit:global:{user_type}")
        elif self.rule_type == 'PATH':
            ConfigService.delete("ratelimit:path:all")
        elif self.rule_type == 'SMS':
            ConfigService.delete("ratelimit:sms")
        elif self.rule_type == 'ANONYMOUS_CHAT':
            ConfigService.delete("ratelimit:anonymous_chat")

        try:
            current = cache.get("ratelimit:path_strategy_version", 0)
            cache.set("ratelimit:path_strategy_version", current + 1, timeout=None)
        except Exception:
            logger.warning("Failed to increment path_strategy_version", exc_info=True)
