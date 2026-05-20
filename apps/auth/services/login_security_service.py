from django.core.cache import cache
from django.utils import timezone
from django.contrib.auth import get_user_model
import logging

User = get_user_model()
logger = logging.getLogger(__name__)


class LoginSecurityService:
    """登录安全服务：处理登录失败、账户锁定和失败记录清理"""

    FAILED_LOGIN_KEY = "failed_login:{user_id}"
    ACCOUNT_LOCK_KEY = "account_lock:{user_id}"
    LOCK_THRESHOLD = 5
    LOCK_DURATION_SECONDS = 900
    FAILED_LOGIN_TTL = 3600

    def handle_failed_login(self, username: str) -> None:
        """处理登录失败，递增失败尝试次数"""
        if not username:
            return

        try:
            user = User.objects.get(username=username)
            self.increment_failed_attempts(user.id)
        except User.DoesNotExist:
            pass

    def increment_failed_attempts(self, user_id: int) -> None:
        """递增失败次数，达到阈值时锁定账户"""
        key = self.FAILED_LOGIN_KEY.format(user_id=user_id)
        attempts = cache.get(key, 0) + 1
        cache.set(key, attempts, self.FAILED_LOGIN_TTL)

        if attempts >= self.LOCK_THRESHOLD:
            lock_key = self.ACCOUNT_LOCK_KEY.format(user_id=user_id)
            lock_until = int(timezone.now().timestamp()) + self.LOCK_DURATION_SECONDS
            cache.set(lock_key, lock_until, self.LOCK_DURATION_SECONDS)

    def clear_failed_attempts(self, user_id: int) -> None:
        """清除失败记录和锁定状态"""
        cache.delete(self.FAILED_LOGIN_KEY.format(user_id=user_id))
        cache.delete(self.ACCOUNT_LOCK_KEY.format(user_id=user_id))

    def is_account_locked(self, user_id: int) -> bool:
        """检查账户是否被锁定"""
        lock_key = self.ACCOUNT_LOCK_KEY.format(user_id=user_id)
        lock_until = cache.get(lock_key)
        if lock_until is None:
            return False

        current_time = int(timezone.now().timestamp())
        return current_time < lock_until

    def get_failed_attempts(self, user_id: int) -> int:
        """获取当前失败次数"""
        key = self.FAILED_LOGIN_KEY.format(user_id=user_id)
        return cache.get(key, 0)

    def get_lock_remaining_minutes(self, user_id: int) -> int:
        """获取账户锁定的剩余分钟数"""
        lock_key = self.ACCOUNT_LOCK_KEY.format(user_id=user_id)
        lock_until = cache.get(lock_key)
        if lock_until is None:
            return 0
        current_time = int(timezone.now().timestamp())
        remaining_seconds = lock_until - current_time
        return max(0, int(remaining_seconds / 60) + 1)

    def handle_failed_login_by_phone(self, phone: str) -> None:
        """处理通过手机号的登录失败，递增失败尝试次数"""
        if not phone:
            return

        try:
            import hashlib
            phone_hash = hashlib.sha256(phone.encode('utf-8')).hexdigest()
            user = User.objects.get(phone_hash=phone_hash)
            self.increment_failed_attempts(user.id)
        except User.DoesNotExist:
            pass
        except Exception:
            logger.warning("handle_failed_login_by_phone failed for phone=%s", phone[:3] + '****', exc_info=True)
