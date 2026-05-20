from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.config.models import BaseConfig


class BrandConfig(BaseConfig):
    """Brand information configuration"""

    DISPLAY_NAMES = {
        'brand_name': '品牌名称',
        'brand_slogan': '品牌标语',
        'brand_contact_email': '联系邮箱',
        'brand_contact_phone': '联系电话',
        'brand_address': '联系地址',
        'brand_logo_url': '品牌 Logo',
        'brand_favicon_url': '网站图标',
        'brand_wechat_qr': '微信二维码',
        'brand_icp_number': 'ICP备案号',
        'brand_copyright_text': '版权信息',
        'brand_welcome_message': '欢迎语',
        'brand_about_us': '关于我们',
    }

    key = models.CharField(_("配置键"), max_length=50, unique=True)
    value = models.CharField(_("配置值"), max_length=500)
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("品牌配置")
        verbose_name_plural = verbose_name

    @property
    def display_name(self):
        """获取配置项的中文显示名称"""
        return self.DISPLAY_NAMES.get(self.key, self.key)

    def __str__(self):
        return f"{self.display_name} ({self.key}) = {self.value}"

    def invalidate_cache(self):
        from django.core.cache import caches
        cache = caches['default']
        try:
            cache.delete_pattern("config:brand_config*")
        except AttributeError:
            cache.delete("config:brand_config")
