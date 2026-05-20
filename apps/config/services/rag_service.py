from apps.config.services import ConfigService
from apps.config.models import RAGConfig


class RAGConfigService:
    """RAG configuration service"""

    @classmethod
    def get_vector_config(cls) -> dict:
        """Get vectorization config"""
        return {
            'vector_dimensions': cls.get_config('vector_dimensions', 1024),
        }

    @classmethod
    def get_chunk_strategy(cls) -> dict:
        """Get chunking strategy"""
        return {
            'chunk_size': cls.get_config('chunk_size', 500),
            'chunk_overlap': cls.get_config('chunk_overlap', 50),
        }

    @classmethod
    def get_retrieval_config(cls) -> dict:
        """Get retrieval config"""
        return {
            'similarity_threshold': cls.get_config('similarity_threshold', 0.35),
            'top_k': cls.get_config('top_k', 3),
        }

    @classmethod
    def get_operation_config(cls) -> dict:
        """Get operation config"""
        return {
            'review_cycle_days': cls.get_config('review_cycle_days', 180),
            'review_reminder_days': cls.get_config('review_reminder_days', 30),
        }

    @classmethod
    def get_config(cls, key: str, default=None):
        """Get single config value"""
        cache_key = f"rag_{key}"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        config = RAGConfig.objects.filter(config_key=key, is_active=True).first()
        if config:
            result = config.config_value
            ConfigService.set(cache_key, result)
            return result

        return default
