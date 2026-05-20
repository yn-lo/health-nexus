import logging
from apps.config.defaults import (
    DEFAULT_LLM_PROVIDERS,
    DEFAULT_EMBEDDING_PROVIDERS,
    DEFAULT_RERANK_PROVIDERS,
    DEFAULT_SENSITIVE_WORDS,
    DEFAULT_SAFETY_RULES,
    DEFAULT_RATE_LIMIT_RULES,
    DEFAULT_RAG_CONFIGS,
    DEFAULT_SYSTEM_CONFIGS,
    DEFAULT_BRAND_CONFIGS,
    DEFAULT_PROMPT_TEMPLATES,
)

logger = logging.getLogger(__name__)


class ConfigInitializer:
    """
    启动时配置初始化服务。
    检查数据库并导入默认配置（仅当配置为空时）。
    幂等安全，不会覆盖已存在的配置。
    """

    @classmethod
    def run(cls):
        from django.conf import settings
        if getattr(settings, 'SKIP_CONFIG_INIT', False):
            logger.info("Config initialization skipped (SKIP_CONFIG_INIT=1)")
            return

        logger.info("Checking default configurations...")
        cls._init_llm_providers()
        cls._init_embedding_providers()
        cls._init_rerank_providers()
        cls._init_sensitive_words()
        cls._init_safety_rules()
        cls._init_rate_limit_rules()
        cls._init_rag_configs()
        cls._init_system_configs()
        cls._init_brand_configs()
        cls._init_prompt_templates()
        logger.info("Default configuration check completed")

    @classmethod
    def _init_llm_providers(cls):
        from apps.config.models import LLMProviderConfig
        if LLMProviderConfig.objects.exists():
            logger.info("  LLM providers: already exists, skipped")
            return
        count = 0
        for cfg in DEFAULT_LLM_PROVIDERS:
            LLMProviderConfig.objects.get_or_create(name=cfg["name"], defaults=cfg)
            count += 1
        logger.info(f"  LLM providers: {count} defaults imported")

    @classmethod
    def _init_embedding_providers(cls):
        from apps.config.models import EmbeddingProviderConfig
        if EmbeddingProviderConfig.objects.exists():
            logger.info("  Embedding providers: already exists, skipped")
            return
        count = 0
        for cfg in DEFAULT_EMBEDDING_PROVIDERS:
            EmbeddingProviderConfig.objects.get_or_create(name=cfg["name"], defaults=cfg)
            count += 1
        logger.info(f"  Embedding providers: {count} defaults imported")

    @classmethod
    def _init_rerank_providers(cls):
        from apps.config.models import RerankProviderConfig
        if RerankProviderConfig.objects.exists():
            logger.info("  Rerank providers: already exists, skipped")
            return
        count = 0
        for cfg in DEFAULT_RERANK_PROVIDERS:
            RerankProviderConfig.objects.get_or_create(name=cfg["name"], defaults=cfg)
            count += 1
        logger.info(f"  Rerank providers: {count} defaults imported")

    @classmethod
    def _init_sensitive_words(cls):
        from apps.config.models import SensitiveWord
        if SensitiveWord.objects.exists():
            logger.info("  Sensitive words: already exists, skipped")
            return
        count = 0
        for word, category in DEFAULT_SENSITIVE_WORDS:
            SensitiveWord.objects.get_or_create(
                word=word, defaults={"category": category}
            )
            count += 1
        logger.info(f"  Sensitive words: {count} defaults imported")

    @classmethod
    def _init_safety_rules(cls):
        from apps.config.models import SafetyRule
        if SafetyRule.objects.exists():
            logger.info("  Safety rules: already exists, skipped")
            return
        count = 0
        for rule in DEFAULT_SAFETY_RULES:
            SafetyRule.objects.get_or_create(
                rule_key=rule["rule_key"], defaults=rule
            )
            count += 1
        logger.info(f"  Safety rules: {count} defaults imported")

    @classmethod
    def _init_rate_limit_rules(cls):
        from apps.config.models import RateLimitRule
        if RateLimitRule.objects.exists():
            logger.info("  Rate limit rules: already exists, skipped")
            return
        count = 0
        for rule in DEFAULT_RATE_LIMIT_RULES:
            RateLimitRule.objects.get_or_create(
                name=rule["name"], defaults=rule
            )
            count += 1
        logger.info(f"  Rate limit rules: {count} defaults imported")

    @classmethod
    def _init_rag_configs(cls):
        from apps.config.models import RAGConfig
        if RAGConfig.objects.exists():
            logger.info("  RAG configs: already exists, skipped")
            return
        count = 0
        for config_key, config_value, category, description in DEFAULT_RAG_CONFIGS:
            RAGConfig.objects.get_or_create(
                config_key=config_key,
                defaults={
                    "config_value": config_value,
                    "category": category,
                    "config_type": "number",
                    "description": description,
                },
            )
            count += 1
        logger.info(f"  RAG configs: {count} defaults imported")

    @classmethod
    def _init_system_configs(cls):
        from apps.config.models import SystemConfig
        if SystemConfig.objects.exists():
            logger.info("  System configs: already exists, skipped")
            return
        count = 0
        for category, config_key, config_value, description in DEFAULT_SYSTEM_CONFIGS:
            SystemConfig.objects.get_or_create(
                category=category,
                config_key=config_key,
                defaults={
                    "config_value": config_value,
                    "description": description,
                },
            )
            count += 1
        logger.info(f"  System configs: {count} defaults imported")

    @classmethod
    def _init_brand_configs(cls):
        from apps.config.models import BrandConfig
        if BrandConfig.objects.exists():
            logger.info("  Brand configs: already exists, skipped")
            return
        count = 0
        for key, value in DEFAULT_BRAND_CONFIGS:
            BrandConfig.objects.get_or_create(
                key=key, defaults={"value": value}
            )
            count += 1
        logger.info(f"  Brand configs: {count} defaults imported")

    @classmethod
    def _init_prompt_templates(cls):
        from apps.config.models import PromptTemplate
        if PromptTemplate.objects.exists():
            logger.info("  Prompt templates: already exists, skipped")
            return
        count = 0
        for tpl in DEFAULT_PROMPT_TEMPLATES:
            PromptTemplate.objects.get_or_create(
                name=tpl["name"], defaults=tpl
            )
            count += 1
        logger.info(f"  Prompt templates: {count} defaults imported")
