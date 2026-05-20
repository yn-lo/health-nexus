from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.base.models import TimeStampedModel


class BaseConfig(TimeStampedModel):
    """Abstract base class for all configuration models"""

    is_active = models.BooleanField(_("是否启用"), default=True)

    class Meta:
        abstract = True

    def save(self, *args, **kwargs):
        super().save(*args, **kwargs)
        self.invalidate_cache()

    def invalidate_cache(self):
        pass
