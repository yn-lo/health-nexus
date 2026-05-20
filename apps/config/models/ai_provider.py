from django.db import models
from django.core.validators import MinValueValidator, MaxValueValidator
from django.utils.translation import gettext_lazy as _
from apps.config.models.base import BaseConfig


class LLMProviderConfig(BaseConfig):
    """LLM 服务提供商配置"""

    PROVIDERS = [
        ('openai', 'OpenAI'),
        ('custom', '自定义'),
    ]

    name = models.CharField(_("配置名称"), max_length=50, unique=True)
    api_key = models.CharField(_("API Key"), max_length=500)
    base_url = models.URLField(_("服务地址"), max_length=500)
    model_name = models.CharField(_("模型名称"), max_length=100)
    provider = models.CharField(_("提供商"), max_length=20, choices=PROVIDERS, default='openai')
    timeout = models.PositiveIntegerField(_("超时时间(秒)"), default=60)
    max_retries = models.PositiveIntegerField(_("最大重试次数"), default=3)
    temperature = models.FloatField(_("Temperature"), default=0.1, validators=[
        MinValueValidator(0.0), MaxValueValidator(2.0)
    ])
    max_tokens = models.PositiveIntegerField(_("最大输出Token数"), default=1024)
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("LLM 配置")
        verbose_name_plural = verbose_name

    def __str__(self):
        return f"{self.name} - {self.model_name}"


class EmbeddingProviderConfig(BaseConfig):
    """Embedding 服务提供商配置"""

    PROVIDERS = [
        ('openai', 'OpenAI'),
        ('custom', '自定义'),
    ]

    name = models.CharField(_("配置名称"), max_length=50, unique=True)
    api_key = models.CharField(_("API Key"), max_length=500)
    base_url = models.URLField(_("服务地址"), max_length=500)
    model_name = models.CharField(_("模型名称"), max_length=100)
    dimensions = models.PositiveIntegerField(
        _("向量维数"),
        default=1024,
        help_text=_("Embedding 向量的维度，如 1024、1536、3072 等")
    )
    provider = models.CharField(_("提供商"), max_length=20, choices=PROVIDERS, default='openai')
    timeout = models.PositiveIntegerField(_("超时时间(秒)"), default=60)
    max_retries = models.PositiveIntegerField(_("最大重试次数"), default=3)
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("Embedding 配置")
        verbose_name_plural = verbose_name

    def __str__(self):
        return f"{self.name} - {self.model_name} ({self.dimensions}d"


class RerankProviderConfig(BaseConfig):
    """Rerank 服务提供商配置"""

    PROVIDERS = [
        ('openai', 'OpenAI'),
        ('custom', '自定义'),
    ]

    name = models.CharField(_("配置名称"), max_length=50, unique=True)
    api_key = models.CharField(_("API Key"), max_length=500)
    base_url = models.URLField(_("服务地址"), max_length=500)
    model_name = models.CharField(_("模型名称"), max_length=100)
    top_n = models.PositiveIntegerField(
        _("默认返回数量"),
        default=5,
        help_text=_("Rerank API 默认返回的文档数量")
    )
    provider = models.CharField(_("提供商"), max_length=20, choices=PROVIDERS, default='openai')
    timeout = models.PositiveIntegerField(_("超时时间(秒)"), default=60)
    max_retries = models.PositiveIntegerField(_("最大重试次数"), default=3)
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("Rerank 配置")
        verbose_name_plural = verbose_name

    def __str__(self):
        return f"{self.name} - {self.model_name}"
