from typing import Dict, Any, Optional
from io import BytesIO
from django.core.cache import cache
from django.core.files.uploadedfile import UploadedFile, InMemoryUploadedFile
from django.utils import timezone
from PIL import Image
from apps.auth.services.sms_service import SMSService
from apps.auth.models import UserProfile

AVATAR_MAX_SIZE = 300
AVATAR_QUALITY = 85


class UserSettingsService:
    def __init__(self, sms_service: SMSService = None):
        self._sms_service = sms_service or SMSService()

    def record_login_device(self, user, ip_address: str, user_agent: str = None) -> bool:
        if not user or not user.login_alert_enabled:
            return False

        cache_key = f"login_device:{user.id}"
        login_info = {
            'ip': ip_address,
            'user_agent': user_agent or '',
            'timestamp': timezone.now().isoformat(),
        }
        cache.set(cache_key, login_info, timeout=86400 * 30)
        return True

    def bind_phone(self, user, phone: str, code: str) -> bool:
        if not self._sms_service.verify_code(phone, code):
            return False

        user.phone = phone
        user.save(update_fields=['phone'])
        return True

    def unbind_phone(self, user, code: str) -> bool:
        if not user.phone:
            return False

        if not self._sms_service.verify_code(user.phone, code):
            return False

        user.phone = ''
        user.save(update_fields=['phone'])
        return True

    def update_security_settings(self, user, **kwargs) -> bool:
        allowed_fields = ['login_alert_enabled']
        update_fields = []

        for field in allowed_fields:
            if field in kwargs:
                setattr(user, field, kwargs[field])
                update_fields.append(field)

        if update_fields:
            user.save(update_fields=update_fields)

        return True

    def update_privacy_settings(self, user, **kwargs) -> bool:
        allowed_fields = ['health_data_share_enabled']
        update_fields = []

        for field in allowed_fields:
            if field in kwargs:
                setattr(user, field, kwargs[field])
                update_fields.append(field)

        if update_fields:
            user.save(update_fields=update_fields)

        return True

    def export_user_data(self, user) -> Dict[str, Any]:
        return {
            'user_id': user.id,
            'username': user.username,
            'email': user.email,
            'role': user.role,
            'phone': user.phone or '',
            'departments': [d.name for d in user.departments.all()] if user.departments.exists() else [],
            'settings': {
                'login_alert_enabled': user.login_alert_enabled,
                'health_data_share_enabled': user.health_data_share_enabled,
            },
        }

    @staticmethod
    def _compress_avatar(uploaded_file: UploadedFile) -> InMemoryUploadedFile:
        img = Image.open(uploaded_file).convert("RGBA")

        width, height = img.size
        if width > AVATAR_MAX_SIZE or height > AVATAR_MAX_SIZE:
            ratio = AVATAR_MAX_SIZE / max(width, height)
            new_width = int(width * ratio)
            new_height = int(height * ratio)
            img = img.resize((new_width, new_height), Image.LANCZOS)

        output = BytesIO()
        img.save(output, format="PNG", optimize=True, quality=AVATAR_QUALITY)
        output.seek(0)

        return InMemoryUploadedFile(
            file=output,
            field_name='avatar',
            name=f"avatar_{uploaded_file.name.rsplit('.', 1)[0]}.png",
            content_type='image/png',
            size=output.tell(),
            charset=None,
        )

    def update_avatar(self, user, avatar_file: UploadedFile) -> bool:
        compressed = self._compress_avatar(avatar_file)
        user.avatar = compressed
        user.save(update_fields=['avatar'])
        return True

    def get_user_preferences(self, user) -> Dict[str, Any]:
        defaults = {
            'theme': 'light',
            'language': 'zh-CN',
            'notification_enabled': True,
            'font_size': 'medium',
        }
        if user.settings:
            defaults.update(user.settings)
        return defaults

    def update_user_preferences(self, user, preferences: Dict[str, Any]) -> bool:
        allowed_keys = {'theme', 'language', 'notification_enabled', 'font_size'}
        valid_preferences = {k: v for k, v in preferences.items() if k in allowed_keys}

        if not valid_preferences:
            return False

        current = user.settings or {}
        current.update(valid_preferences)
        user.settings = current
        user.save(update_fields=['settings'])
        return True
