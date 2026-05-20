import logging

from django.apps import AppConfig
from django.db.backends.signals import connection_created
from django.dispatch import receiver
from django.utils.translation import gettext_lazy as _

logger = logging.getLogger(__name__)


def _ensure_pgvector_extension(sender, connection, **kwargs):
    if connection.vendor != 'postgresql':
        return

    try:
        with connection.cursor() as cursor:
            cursor.execute("SELECT 1 FROM pg_extension WHERE extname = 'vector';")
            if cursor.fetchone():
                logger.info("pgvector extension is already installed.")
                return

            cursor.execute("CREATE EXTENSION IF NOT EXISTS vector;")
            logger.info("pgvector extension installed successfully.")
    except Exception as e:
        logger.warning("Failed to ensure pgvector extension: %s", e)


class BaseConfig(AppConfig):
    default_auto_field = 'django.db.models.BigAutoField'
    name = 'apps.base'
    verbose_name = _('基础服务')

    def ready(self):
        import apps.base.signals

        connection_created.connect(_ensure_pgvector_extension)

        try:
            from apps.service_container import initialize_real_services
            initialize_real_services()
        except Exception:
            logger.exception("Failed to initialize services")
