from django.apps import AppConfig
from django.utils.translation import gettext_lazy as _

class CareConfig(AppConfig):
    default_auto_field = 'django.db.models.BigAutoField'
    name = 'apps.care'
    verbose_name = _('患者档案')
