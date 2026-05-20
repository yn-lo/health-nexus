import time
import logging
from typing import Protocol
from django.core.cache import cache

logger = logging.getLogger(__name__)


class RateLimitStrategy(Protocol):
    def is_allowed(self, identifier: str) -> tuple[bool, int]: ...

    def get_retry_after(self, identifier: str) -> int: ...


class FixedWindowRateLimitStrategy:
    def __init__(self, limit: int, window_seconds: int):
        self._limit = limit
        self._window = window_seconds

    def _make_key(self, identifier: str) -> str:
        window = int(time.time()) // self._window
        return f"ratelimit:{identifier}:{window}"

    def is_allowed(self, identifier: str) -> tuple[bool, int]:
        key = self._make_key(identifier)

        try:
            current = cache.incr(key)
        except ValueError:
            added = cache.add(key, 1, self._window)
            if added:
                return True, self._limit - 1
            try:
                current = cache.incr(key)
            except ValueError:
                return False, 0

        if current > self._limit:
            return False, 0
        return True, max(0, self._limit - current)

    def get_retry_after(self, identifier: str) -> int:
        key = self._make_key(identifier)
        try:
            ttl = cache.ttl(key)
            return max(0, ttl)
        except (AttributeError, TypeError):
            return self._window


class PathBasedRateLimitStrategy:
    def __init__(self, path_limits: dict):
        self._path_limits = path_limits
        self._strategies = {}
        for path_config_key, config in path_limits.items():
            limit = config.get('limit', 10)
            window = config.get('window', 60)
            self._strategies[path_config_key] = FixedWindowRateLimitStrategy(limit, window)
        self._sorted_keys = sorted(
            self._path_limits.keys(),
            key=lambda k: len(self._path_limits[k].get('path', '')),
            reverse=True,
        )

    def _find_matching_key(self, path: str, method: str):
        for key in self._sorted_keys:
            config = self._path_limits[key]
            config_path = config.get('path', '')
            aliases = config.get('aliases', [])
            matched = path.startswith(config_path)
            if not matched:
                for alias in aliases:
                    if path.startswith(alias):
                        matched = True
                        break
            if matched:
                allowed_methods = config.get('methods', ['GET', 'POST'])
                if method in allowed_methods:
                    return key
        return None

    def is_allowed(self, path: str, method: str, identifier: str) -> tuple[bool, int, int]:
        matching_key = self._find_matching_key(path, method)
        if not matching_key:
            return True, 0, 0

        config = self._path_limits[matching_key]
        strategy = self._strategies[matching_key]
        cache_key = f"{method}:{config.get('path', '')}:{identifier}"
        allowed, remaining = strategy.is_allowed(cache_key)
        retry_after = strategy.get_retry_after(cache_key) if not allowed else 0
        return allowed, remaining, retry_after

    def get_retry_after(self, path: str, method: str, identifier: str) -> int:
        matching_key = self._find_matching_key(path, method)
        if matching_key:
            config = self._path_limits[matching_key]
            cache_key = f"{method}:{config.get('path', '')}:{identifier}"
            return self._strategies[matching_key].get_retry_after(cache_key)
        return 60
