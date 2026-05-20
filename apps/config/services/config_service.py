from django.core.cache import cache


class ConfigService:
    """Cache-first configuration read service: Cache -> Caller-provided value -> Default"""

    @classmethod
    def get(cls, key, default=None, timeout=3600):
        cache_key = f"config:{key}" if not key.startswith("config:") else key
        value = cache.get(cache_key)
        if value is not None:
            return value
        return default

    @classmethod
    def set(cls, key, value, timeout=3600):
        cache_key = f"config:{key}" if not key.startswith("config:") else key
        cache.set(cache_key, value, timeout=timeout)

    @classmethod
    def delete(cls, key):
        cache_key = f"config:{key}" if not key.startswith("config:") else key
        cache.delete(cache_key)

    @classmethod
    def delete_pattern(cls, pattern):
        from django.core.cache import caches
        cache = caches['default']
        if hasattr(cache, 'delete_pattern'):
            cache.delete_pattern(pattern)
