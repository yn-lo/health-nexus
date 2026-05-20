import logging
from typing import Optional
from django.core.cache import cache

logger = logging.getLogger(__name__)


class AIConfigService:

    @classmethod
    def get_all_active_configs(cls, provider_type: str):
        if provider_type == 'LLM':
            from apps.config.models import LLMProviderConfig
            return list(
                LLMProviderConfig.objects.filter(is_active=True).order_by('id').values(
                    'name', 'api_key', 'base_url', 'model_name',
                    'timeout', 'max_retries', 'temperature', 'max_tokens', 'provider'
                )
            )
        elif provider_type == 'EMBEDDING':
            from apps.config.models import EmbeddingProviderConfig
            return list(
                EmbeddingProviderConfig.objects.filter(is_active=True).order_by('id').values(
                    'name', 'api_key', 'base_url', 'model_name',
                    'timeout', 'max_retries', 'dimensions', 'provider'
                )
            )
        elif provider_type == 'RERANK':
            from apps.config.models import RerankProviderConfig
            return list(
                RerankProviderConfig.objects.filter(is_active=True).order_by('id').values(
                    'name', 'api_key', 'base_url', 'model_name',
                    'timeout', 'max_retries', 'top_n', 'provider'
                )
            )
        return []

    @classmethod
    def get_next_config(cls, provider_type: str) -> dict:
        """统一入口，provider_type = 'LLM' 或 'EMBEDDING'"""
        configs = cls.get_all_active_configs(provider_type)
        if not configs:
            raise ValueError(f"No active {provider_type} configuration found")

        cache_key = f'ai:{provider_type.lower()}:round_robin_index'
        current_index = cache.get(cache_key, 0)
        config = configs[current_index % len(configs)]
        cache.set(cache_key, (current_index + 1) % len(configs), timeout=3600)
        return config

    @classmethod
    def get_llm_config(cls) -> dict:
        """获取 LLM 配置"""
        return cls.get_next_config('LLM')

    @classmethod
    def get_embedding_config(cls) -> dict:
        """获取 Embedding 配置"""
        return cls.get_next_config('EMBEDDING')

    @classmethod
    def get_rerank_config(cls) -> dict:
        """获取 Rerank 配置"""
        return cls.get_next_config('RERANK')

    @classmethod
    def has_active_rerank_config(cls) -> bool:
        """检查是否存在活跃的 Rerank 配置"""
        return len(cls.get_all_active_configs('RERANK')) > 0

    @classmethod
    def invalidate_cache(cls, provider_type: Optional[str] = None):
        """清除轮询索引缓存"""
        if provider_type:
            cache.delete(f'ai:{provider_type.lower()}:round_robin_index')
        else:
            cache.delete('ai:llm:round_robin_index')
            cache.delete('ai:embedding:round_robin_index')
            cache.delete('ai:rerank:round_robin_index')
