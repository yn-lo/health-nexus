from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.config.models import BaseConfig


class SafetyRule(BaseConfig):

    RULE_TYPES = [
        ('DANGEROUS_OUTPUT', '危险输出模式'),
        ('SIMILARITY_THRESHOLD', '相似度阈值'),
        ('REJECTION_MESSAGE', '拒答话术'),
        ('EMERGENCY_RESPONSE', '紧急响应话术'),
        ('SAFETY_WARNING', '安全警告话术'),
    ]

    rule_type = models.CharField(_("规则类型"), max_length=30, choices=RULE_TYPES)
    rule_key = models.CharField(_("规则标识"), max_length=50, unique=True)
    rule_value = models.JSONField(_("规则值"))
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("安全规则")
        verbose_name_plural = verbose_name

    def __str__(self):
        return f"{self.get_rule_type_display()} - {self.rule_key}"

    CACHE_KEY_MAP = {
        'DANGEROUS_OUTPUT': 'dangerous_patterns',
        'SIMILARITY_THRESHOLD': 'similarity_threshold',
        'REJECTION_MESSAGE': 'safety_REJECTION_MESSAGE',
        'EMERGENCY_RESPONSE': 'safety_EMERGENCY_RESPONSE',
        'SAFETY_WARNING': 'safety_SAFETY_WARNING',
    }

    def invalidate_cache(self):
        from apps.config.services.config_service import ConfigService
        cache_key = self.CACHE_KEY_MAP.get(self.rule_type)
        if cache_key:
            ConfigService.delete(cache_key)
