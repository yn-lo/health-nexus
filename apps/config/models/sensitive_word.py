from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.config.models import BaseConfig


class SensitiveWord(BaseConfig):
    """Sensitive word management"""

    CATEGORIES = [
        ('SUICIDE', '自杀自残'),
        ('EMERGENCY', '紧急医疗'),
        ('DRUG', '药物剂量'),
        ('DIAGNOSIS', '诊断治疗'),
    ]

    word = models.CharField(_("敏感词"), max_length=50, unique=True)
    category = models.CharField(_("分类"), max_length=20, choices=CATEGORIES)

    class Meta:
        verbose_name = _("敏感词")
        verbose_name_plural = verbose_name
        ordering = ['category', 'word']

    def __str__(self):
        return self.word

    def invalidate_cache(self):
        from django.core.cache import cache
        try:
            cache.delete_pattern("config:sensitive_words:*")
        except AttributeError:
            cache.delete("config:sensitive_words:all")
            cache.delete("config:sensitive_words:SUICIDE")
            cache.delete("config:sensitive_words:EMERGENCY")
            cache.delete("config:sensitive_words:DRUG")
            cache.delete("config:sensitive_words:DIAGNOSIS")
