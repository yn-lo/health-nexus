from django.db import models
from django.utils.translation import gettext_lazy as _
from apps.config.models import BaseConfig


class RAGConfig(BaseConfig):
    """RAG vectorization and retrieval configuration"""

    CATEGORIES = [
        ('VECTOR', '向量参数'),
        ('CHUNK', '切片策略'),
        ('RETRIEVAL', '检索策略'),
        ('OPERATION', '文章运营'),
    ]

    DISPLAY_NAMES = {
        'embedding_dimension': '向量维度',
        'batch_size': '批次大小',
        'chunk_size': '切片大小',
        'chunk_overlap': '切片重叠',
        'max_chunks': '最大切片数',
        'top_k': '检索数量',
        'rerank_enabled': 'Rerank 开关',
        'rerank_threshold': '重排阈值',
        'diversity_factor': '多样性因子',
        'max_articles_per_day': '每日文章上限',
        'min_quality_score': '最低质量分',
        'auto_publish': '自动发布',
    }

    config_key = models.CharField(_("配置项"), max_length=50, unique=True)
    config_value = models.FloatField(_("配置值"))
    config_type = models.CharField(_("值类型"), max_length=20, default='number')
    category = models.CharField(_("分类"), max_length=20, choices=CATEGORIES)
    min_value = models.FloatField(_("最小值"), null=True, blank=True)
    max_value = models.FloatField(_("最大值"), null=True, blank=True)
    description = models.CharField(_("说明"), max_length=200, blank=True)

    class Meta:
        verbose_name = _("RAG 配置")
        verbose_name_plural = verbose_name

    @property
    def display_name(self):
        """获取配置项的中文显示名称"""
        return self.DISPLAY_NAMES.get(self.config_key, self.config_key)

    def __str__(self):
        return f"{self.display_name} ({self.config_key}) = {self.config_value}"

    def invalidate_cache(self):
        from django.core.cache import caches
        cache = caches['default']
        cache_key = f"config:rag_{self.config_key}"
        try:
            cache.delete_pattern(f"{cache_key}*")
        except AttributeError:
            cache.delete(cache_key)
