from django.apps import AppConfig
from django.utils.translation import gettext_lazy as _


class StatsConfig(AppConfig):
    default_auto_field = 'django.db.models.BigAutoField'
    name = 'apps.stats'
    verbose_name = _('运营统计')

    def ready(self):
        """注册定时任务"""
        pass
