from django.apps import AppConfig
from django.utils.translation import gettext_lazy as _

class WikiConfig(AppConfig):
    default_auto_field = 'django.db.models.BigAutoField'
    name = 'apps.wiki'
    verbose_name = _('知识库')

    def ready(self):
        import apps.wiki.signals
