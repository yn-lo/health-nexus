from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.config.models import BaseConfig


class PromptTemplate(BaseConfig):
    name = models.CharField(_("模板名称"), max_length=100, unique=True)
    content = models.TextField(_("模板内容"), help_text=_("支持变量替换，如 {{patient_context}}、{{department}}"))
    is_default = models.BooleanField(_("是否活跃模板"), default=False, help_text=_("同一时刻只能有一个活跃模板"))
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("Prompt 模板")
        verbose_name_plural = _("Prompt 模板")
        ordering = ['-is_default', 'name']

    def __str__(self):
        active_marker = " [活跃]" if self.is_default else ""
        return f"{self.name}{active_marker}"

    def save(self, *args, **kwargs):
        if self.is_default:
            PromptTemplate.objects.filter(is_default=True).exclude(pk=self.pk).update(is_default=False)
        super().save(*args, **kwargs)

    def invalidate_cache(self):
        from apps.config.services.config_service import ConfigService
        ConfigService.delete("prompt_template:active")
