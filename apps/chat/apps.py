from django.apps import AppConfig
from django.utils.translation import gettext_lazy as _
import logging

logger = logging.getLogger(__name__)


class ChatConfig(AppConfig):
    default_auto_field = 'django.db.models.BigAutoField'
    name = 'apps.chat'
    verbose_name = _('智能问答')

    def ready(self):
        from django.db.models.signals import post_migrate
        post_migrate.connect(self._check_ai_config, sender=self)

    def _check_ai_config(self, sender, **kwargs):
        from django.conf import settings
        import sys

        if getattr(sys, '_called_from_test', False) or 'pytest' in sys.modules or 'test' in sys.argv:
            return

        if not settings.DEBUG:
            from apps.config.models import LLMProviderConfig, EmbeddingProviderConfig
            try:
                llm_count = LLMProviderConfig.objects.filter(is_active=True).count()
                emb_count = EmbeddingProviderConfig.objects.filter(is_active=True).count()
                if llm_count == 0:
                    logger.warning(
                        "Production mode requires at least one active LLM provider configuration. "
                        "Add one via Admin > 系统配置 > LLM服务配置."
                    )
                if emb_count == 0:
                    logger.warning(
                        "Production mode requires at least one active Embedding provider configuration. "
                        "Add one via Admin > 系统配置 > Embedding服务配置."
                    )
            except Exception as e:
                err_msg = str(e).lower()
                if any(kw in err_msg for kw in ("no such table", "does not exist", "migrations", "relation")):
                    logger.warning("Database not ready yet, skipping AI config check.")
                else:
                    raise
