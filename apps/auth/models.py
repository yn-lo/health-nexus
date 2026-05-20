import logging
import uuid
import qrcode
import base64
from io import BytesIO
from django.db import models
from django.contrib.auth.models import AbstractUser, UserManager
from django.utils.translation import gettext_lazy as _
from django.utils import timezone
from encrypted_model_fields.fields import EncryptedCharField
from apps.base.models import Department

logger = logging.getLogger(__name__)


def generate_qr_image(qr_code: str, site_url: str) -> str:
    """Generate base64 encoded QR code image"""
    try:
        bind_url = f"{site_url}/accounts/bind/{qr_code}/"
        qr = qrcode.QRCode(version=1, box_size=10, border=4)
        qr.add_data(bind_url)
        qr.make(fit=True)
        img = qr.make_image(fill_color="black", back_color="white")
        buffer = BytesIO()
        img.save(buffer, format="PNG")
        return f"data:image/png;base64,{base64.b64encode(buffer.getvalue()).decode()}"
    except Exception:
        logger.exception("Failed to generate QR code image for qr_code=%s", qr_code)
        raise


class UserProfileManager(UserManager):
    def create_superuser(self, username, email=None, password=None, **extra_fields):
        extra_fields.setdefault('is_staff', True)
        extra_fields.setdefault('is_superuser', True)
        extra_fields.setdefault('role', UserProfile.Role.SUPER_ADMIN)
        return super().create_superuser(username, email, password, **extra_fields)


class UserProfile(AbstractUser):
    objects = UserProfileManager()

    class Role(models.TextChoices):
        SUPER_ADMIN = 'SUPER_ADMIN', _('超级管理员')
        DEPT_ADMIN = 'DEPT_ADMIN', _('科室管理员')
        DOCTOR = 'DOCTOR', _('医生')
        NURSE = 'NURSE', _('护士')
        PATIENT = 'PATIENT', _('患者')

    role = models.CharField(_("角色"), max_length=20, choices=Role.choices, default=Role.PATIENT)

    departments = models.ManyToManyField(
        Department,
        through='base.UserDepartment',
        related_name='users',
        verbose_name=_("关联科室"),
        help_text=_("用户所属的多个科室")
    )

    phone = EncryptedCharField(max_length=20, blank=True, verbose_name=_("手机号 (加密)"))
    phone_hash = models.CharField(
        _("手机号哈希"),
        max_length=64,
        blank=True,
        null=True,
        default='',
        db_index=True,
        help_text=_("手机号 SHA-256 哈希，用于快速查找")
    )
    current_session_key = models.CharField(
        _("当前会话 key"),
        max_length=128,
        null=True,
        blank=True,
        unique=True,
        help_text=_("单设备单会话限制，新登录会覆盖旧会话")
    )
    avatar = models.ImageField(
        _("头像"),
        upload_to='avatars/',
        null=True,
        blank=True,
    )

    login_alert_enabled = models.BooleanField(
        _("登录提醒开关"),
        default=True
    )
    health_data_share_enabled = models.BooleanField(
        _("健康数据共享开关"),
        default=True
    )

    agreed_terms = models.BooleanField(
        _("是否同意免责条款"),
        default=False,
        help_text=_("患者首次登录必须同意免责条款")
    )
    agreed_terms_at = models.DateTimeField(
        _("同意条款时间"),
        null=True,
        blank=True
    )

    settings = models.JSONField(
        _("用户偏好设置"),
        default=dict,
        blank=True,
        help_text=_("用户偏好设置: theme, language, notification_enabled, font_size")
    )

    bind_qr_code = models.CharField(
        _("绑定二维码标识"),
        max_length=64,
        unique=True,
        null=True,
        blank=True,
        help_text=_("用户专属绑定二维码标识，注册时自动生成")
    )

    class Title(models.TextChoices):
        DOCTOR = 'doctor', _('医生')
        NURSE = 'nurse', _('护士')
        MEDICAL_TECHNICIAN = 'medical_technician', _('医技')

    title = models.CharField(
        _("职称"),
        max_length=20,
        choices=Title.choices,
        blank=True,
        default='',
    )
    bio = models.TextField(
        _("个人简介"),
        blank=True,
        default='',
        help_text=_("医生/护士个人专业背景介绍")
    )

    class Meta:
        verbose_name = _("用户账户")
        verbose_name_plural = _("用户账户")

    def save(self, *args, **kwargs):
        import hashlib
        self.is_staff = self.role in [self.Role.SUPER_ADMIN, self.Role.DEPT_ADMIN]
        self.is_superuser = self.role == self.Role.SUPER_ADMIN
        if self.phone:
            self.phone_hash = hashlib.sha256(self.phone.encode('utf-8')).hexdigest()
        else:
            self.phone_hash = ''
        super().save(*args, **kwargs)

    def agree_to_terms(self):
        self.agreed_terms = True
        self.agreed_terms_at = timezone.now()
        self.save(update_fields=['agreed_terms', 'agreed_terms_at'])

    @property
    def is_medical_staff(self):
        return self.role in [self.Role.DOCTOR, self.Role.NURSE]

    @property
    def is_admin(self):
        return self.role in [self.Role.SUPER_ADMIN, self.Role.DEPT_ADMIN]

    @property
    def is_patient(self):
        return self.role == self.Role.PATIENT

    @property
    def can_access_admin(self):
        return self.role in [self.Role.SUPER_ADMIN, self.Role.DEPT_ADMIN]

    @property
    def can_access_staff_dashboard(self):
        return self.role in [
            self.Role.DOCTOR, self.Role.NURSE,
            self.Role.SUPER_ADMIN, self.Role.DEPT_ADMIN,
        ]

    @property
    def can_access_frontend(self):
        return self.role == self.Role.PATIENT

    def can_review_articles(self, department=None):
        """验证用户是否有权限审核文章。

        SUPER_ADMIN 始终有审核权限。
        DOCTOR 和 NURSE 可审核其所属科室的文章。
        DEPT_ADMIN 可审核其所属科室的文章。

        Args:
            department: 可选，指定要审核文章的科室。如果为 None，只检查角色权限。

        Returns:
            bool: 是否有审核权限
        """
        from apps.base.models import UserDepartment

        if self.role == self.Role.SUPER_ADMIN:
            return True

        if self.role in (self.Role.DOCTOR, self.Role.NURSE, self.Role.DEPT_ADMIN):
            if department is None:
                return True
            return UserDepartment.objects.filter(
                user=self, department=department
            ).exists()

        return False
