from django.apps import AppConfig


class ConfigAppConfig(AppConfig):
    default_auto_field = 'django.db.models.BigAutoField'
    name = 'apps.config'
    label = 'config'
    verbose_name = '系统配置'

    def ready(self):
        import apps.config.signals  # noqa: F401
        import apps.config.dashboard  # noqa: F401
        self._init_default_configs()

    def _init_default_configs(self):
        from django.db.models.signals import post_migrate
        post_migrate.connect(self._on_post_migrate, sender=self)

    def _on_post_migrate(self, sender, **kwargs):
        from apps.config.services.config_initializer import ConfigInitializer
        ConfigInitializer.run()
