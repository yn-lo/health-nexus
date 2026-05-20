import random
import logging
from abc import ABC, abstractmethod
from django.conf import settings
from apps.config.services import ConfigService, RateLimitConfigService
from apps.config.models import SystemConfig
from apps.auth.rate_limit.strategies import FixedWindowRateLimitStrategy
from typing import Optional
from django.core.cache import cache


FIXED_TEST_CODE = "123456"


class SMSConfigService:
    """SMS configuration service with cache-database-defaults fallback"""

    @classmethod
    def get_config(cls) -> dict:
        """Get SMS config with three-level fallback"""
        cache_key = "sms:config"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        db_config = {}
        for config in SystemConfig.objects.filter(category='SMS'):
            db_config[config.config_key] = config.config_value

        result = {
            'code_length': db_config.get('sms_code_length', 6),
            'code_ttl': db_config.get('sms_code_ttl', 300),
            'max_attempts': db_config.get('sms_max_attempts', 3),
            'lockout_duration': db_config.get('sms_lockout_duration', 300),
        }

        ConfigService.set(cache_key, result)
        return result

    @classmethod
    def invalidate_cache(cls):
        """Invalidate SMS config cache"""
        ConfigService.delete("sms:config")


class SMSCodeStoreProtocol(ABC):
    """Abstract interface for SMS code storage - allows substitution"""

    @abstractmethod
    def store(self, phone: str, code: str, ttl: int = 300) -> None: ...

    @abstractmethod
    def verify(self, phone: str, code: str) -> bool: ...

    @abstractmethod
    def get_code(self, phone: str) -> Optional[str]: ...

    @abstractmethod
    def delete(self, phone: str) -> None: ...


class RedisSMSCodeStore(SMSCodeStoreProtocol):
    """Redis-backed implementation with expiration"""

    def __init__(self, prefix: str = "sms:"):
        self._prefix = prefix

    def _make_key(self, phone: str) -> str:
        return f"{self._prefix}{phone}"

    def store(self, phone: str, code: str, ttl: int = 300) -> None:
        key = self._make_key(phone)
        cache.set(key, code, ttl)

    def verify(self, phone: str, code: str) -> bool:
        stored = self.get_code(phone)
        return stored is not None and stored == code

    def get_code(self, phone: str) -> Optional[str]:
        return cache.get(self._make_key(phone))

    def delete(self, phone: str) -> None:
        cache.delete(self._make_key(phone))


class SMSService:
    """High-level SMS service with security best practices"""

    def __init__(self, store: SMSCodeStoreProtocol = None):
        self._store = store or RedisSMSCodeStore()
        self._config_loaded = False

    def _ensure_config_loaded(self):
        """Lazy-load SMS config on first use"""
        if not self._config_loaded:
            self._load_config()
            self._config_loaded = True

    def _load_config(self):
        """Load SMS config from dynamic config service"""
        config = SMSConfigService.get_config()
        self._code_length = config['code_length']
        self._max_attempts = config['max_attempts']
        self._lockout_duration = config['lockout_duration']
        self._code_ttl = config['code_ttl']

    def _generate_code(self) -> str:
        return str(random.randint(100000, 999999))

    def _check_ip_rate_limit(self, ip: str, sms_limits: dict) -> tuple[bool, int]:
        ip_limit_data = sms_limits.get('ip', {'limit': 10, 'window': 3600})
        ip_limit = ip_limit_data['limit']
        ip_window = ip_limit_data['window']

        strategy = FixedWindowRateLimitStrategy(limit=ip_limit, window_seconds=ip_window)
        identifier = f"ratelimit:sms:ip:{ip}"
        allowed, remaining = strategy.is_allowed(identifier)

        if not allowed:
            retry_after = strategy.get_retry_after(identifier)
            return False, retry_after

        return True, 0

    def _is_test_phone(self, phone: str) -> bool:
        if settings.DEBUG:
            return True
        return phone.startswith("000") or phone == "13800138000"

    def send_code(self, phone: str, ip: str = None) -> dict:
        self._ensure_config_loaded()

        sms_limits = RateLimitConfigService.get_sms_limits()

        if ip:
            allowed, retry_after = self._check_ip_rate_limit(ip, sms_limits)
            if not allowed:
                return {"success": False, "error": f"IP请求过于频繁，请{retry_after}秒后再试", "retry_after": retry_after}

        phone_limit_data = sms_limits.get('phone', {'limit': 5, 'window': 3600})
        phone_limit = phone_limit_data['limit']
        phone_window = phone_limit_data['window']

        phone_strategy = FixedWindowRateLimitStrategy(limit=phone_limit, window_seconds=phone_window)
        phone_identifier = f"ratelimit:sms:phone:{phone}"
        phone_allowed, phone_remaining = phone_strategy.is_allowed(phone_identifier)

        if not phone_allowed:
            retry_after = phone_strategy.get_retry_after(phone_identifier)
            return {"success": False, "error": "该手机号发送过于频繁，请稍后再试", "retry_after": retry_after}

        if self._is_test_phone(phone):
            code = FIXED_TEST_CODE
            self._store.store(phone, code, ttl=600)
        else:
            code = self._generate_code()
            self._store.store(phone, code, ttl=300)

        return {"success": True, "code": code, "is_test_code": self._is_test_phone(phone)}

    def verify_code(self, phone: str, code: str) -> bool:
        self._ensure_config_loaded()
        attempt_key = f"ratelimit:sms:attempts:{phone}"
        failed_attempts = cache.get(attempt_key, 0)

        if failed_attempts >= self._max_attempts:
            return False

        if self._is_test_phone(phone) and code == FIXED_TEST_CODE:
            if self._store.get_code(phone) is None:
                return False
            self._store.delete(phone)
            cache.delete(attempt_key)
            return True

        is_valid = self._store.verify(phone, code)

        if not is_valid:
            cache.set(attempt_key, failed_attempts + 1, self._lockout_duration)
            return False

        self._store.delete(phone)
        cache.delete(attempt_key)
        return True

    def check_code(self, phone: str, code: str) -> bool:
        """校验验证码但不消费（用于表单验证阶段）"""
        self._ensure_config_loaded()
        attempt_key = f"ratelimit:sms:attempts:{phone}"
        failed_attempts = cache.get(attempt_key, 0)

        if failed_attempts >= self._max_attempts:
            return False

        if self._is_test_phone(phone) and code == FIXED_TEST_CODE:
            return self._store.get_code(phone) is not None

        return self._store.verify(phone, code)

    def consume_code(self, phone: str) -> None:
        """消费验证码（在业务操作成功后调用）"""
        if not phone:
            return
        self._store.delete(phone)
        attempt_key = f"ratelimit:sms:attempts:{phone}"
        cache.delete(attempt_key)
