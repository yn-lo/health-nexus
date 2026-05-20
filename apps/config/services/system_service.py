from django.conf import settings
from apps.config.services import ConfigService
from apps.config.models import SystemConfig, BrandConfig


class SystemConfigService:
    """System configuration service"""

    @classmethod
    def get_session_config(cls) -> dict:
        return {
            'session_cookie_age': cls.get_value('SESSION', 'session_cookie_age', 3600),
            'session_expire_browser_close': cls.get_value('SESSION', 'session_expire_browser_close', True),
        }

    @classmethod
    def get_chat_session_retention_days(cls) -> int:
        return cls.get_value('CHAT', 'session_retention_days', 90)

    @classmethod
    def get_task_config(cls) -> dict:
        """Get task queue config"""
        return {
            'task_timeout': cls.get_value('TASK', 'task_timeout', 300),
            'task_max_retries': cls.get_value('TASK', 'task_max_retries', 2),
            'task_retry_delay': cls.get_value('TASK', 'task_retry_delay', 60),
        }

    @classmethod
    def get_network_config(cls) -> dict:
        """Get network config"""
        return {
            'allowed_hosts': cls.get_value('NETWORK', 'allowed_hosts', ['localhost', '127.0.0.1']),
            'csrf_trusted_origins': cls.get_value('NETWORK', 'csrf_trusted_origins', []),
            'cors_allowed_origins': cls.get_value('NETWORK', 'cors_allowed_origins', []),
        }

    @classmethod
    def get_brand_config(cls) -> dict:
        """Get brand config from database with fallback"""
        cache_key = "brand_config"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        brands = BrandConfig.objects.filter(is_active=True)
        result = {b.key: b.value for b in brands}

        if not result:
            result = cls._get_brand_defaults()

        ConfigService.set(cache_key, result)
        return result

    @classmethod
    def get_sms_config(cls) -> dict:
        """Get SMS config"""
        return {
            'sms_code_length': cls.get_value('SMS', 'sms_code_length', 6),
            'sms_code_ttl': cls.get_value('SMS', 'sms_code_ttl', 300),
            'sms_api_key': cls.get_value('SMS', 'sms_api_key', ''),
            'sms_base_url': cls.get_value('SMS', 'sms_base_url', ''),
        }

    @classmethod
    def get_value(cls, category: str, key: str, default=None):
        """Get single system config value"""
        cache_key = f"system_{category}_{key}"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        config = SystemConfig.objects.filter(
            category=category,
            config_key=key
        ).first()

        if config:
            result = config.config_value
            ConfigService.set(cache_key, result)
            return result

        return default

    @classmethod
    def _get_brand_defaults(cls) -> dict:
        from apps.base.brand_config import BRAND_CONFIG_DEFAULTS
        defaults = BRAND_CONFIG_DEFAULTS
        return {
            'brand_name': defaults['name'],
            'brand_tagline': defaults['tagline'],
            'brand_primary_color': defaults['colors']['primary'],
            'brand_owner_name': defaults['owner']['name'],
            'brand_owner_website': defaults['owner']['website'],
            'brand_support_hotline': defaults['support']['hotline'],
            'brand_copyright': defaults['legal']['copyright'],
        }
