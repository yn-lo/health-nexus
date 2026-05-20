"""Session configuration service with cache-database-defaults fallback."""
from apps.config.services import ConfigService
from apps.config.models import SystemConfig


class SessionConfigService:
    """Session configuration service with three-level fallback"""

    @classmethod
    def get_config(cls) -> dict:
        """Get session config with three-level fallback: cache -> DB -> defaults"""
        cache_key = "session:config"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        db_config = {}
        for config in SystemConfig.objects.filter(category='SESSION'):
            db_config[config.config_key] = config.config_value

        result = {
            'cookie_age': db_config.get('session_cookie_age', 3600),
            'expire_browser_close': db_config.get('session_expire_browser_close', True),
        }

        ConfigService.set(cache_key, result)
        return result

    @classmethod
    def invalidate_cache(cls):
        """Invalidate session config cache"""
        ConfigService.delete("session:config")
