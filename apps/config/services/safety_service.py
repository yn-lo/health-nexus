from django.conf import settings
from apps.config.services.config_service import ConfigService
from apps.config.models import SensitiveWord
from apps.config.models.safety_rule import SafetyRule


class SafetyConfigService:
    """Safety configuration service"""

    @classmethod
    def get_sensitive_words(cls, category=None) -> list:
        """Get sensitive words list"""
        cache_key = f"sensitive_words:{category}" if category else "sensitive_words:all"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        queryset = SensitiveWord.objects.filter(is_active=True)
        if category:
            queryset = queryset.filter(category=category)

        words = list(queryset.values_list('word', flat=True))
        ConfigService.set(cache_key, words)
        return words

    @classmethod
    def get_dangerous_patterns(cls) -> list:
        """Get dangerous AI output patterns"""
        cache_key = "dangerous_patterns"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rule = SafetyRule.objects.filter(
            rule_type='DANGEROUS_OUTPUT',
            rule_key='dangerous_patterns',
            is_active=True
        ).first()

        if rule:
            patterns = rule.rule_value if isinstance(rule.rule_value, list) else []
            ConfigService.set(cache_key, patterns)
            return patterns

        patterns = getattr(settings, 'DANGEROUS_AI_OUTPUT_PATTERNS', [
            "你得了", "你患有", "确诊为", "每天吃", "服用", "剂量", "加量", "减量", "停药"
        ])
        ConfigService.set(cache_key, patterns)
        return patterns

    @classmethod
    def get_similarity_threshold(cls) -> float:
        """Get similarity threshold"""
        cache_key = "similarity_threshold"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rule = SafetyRule.objects.filter(
            rule_type='SIMILARITY_THRESHOLD',
            is_active=True
        ).first()

        if rule:
            result = float(rule.rule_value) if isinstance(rule.rule_value, (int, float, str)) else 0.35
            ConfigService.set(cache_key, result)
            return result

        default = getattr(settings, 'SIMILARITY_THRESHOLD', 0.35)
        ConfigService.set(cache_key, default)
        return default

    @classmethod
    def get_rejection_message(cls) -> str:
        """Get rejection message"""
        return cls._get_rule_value('REJECTION_MESSAGE', '抱歉，我的知识库暂时没有相关内容，建议你咨询专业医生获取更准确的建议。')

    @classmethod
    def get_emergency_response(cls) -> str:
        """Get emergency response"""
        return cls._get_rule_value('EMERGENCY_RESPONSE', '这似乎是一个紧急情况，请立即联系专业医疗机构或拨打急救电话。')

    @classmethod
    def get_safety_warning(cls) -> str:
        """Get safety warning"""
        return cls._get_rule_value('SAFETY_WARNING', '以上信息仅供参考，不构成医疗建议。如有健康问题，请咨询专业医生。')

    @classmethod
    def _get_rule_value(cls, rule_type, default):
        cache_key = f"safety_{rule_type}"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rule = SafetyRule.objects.filter(
            rule_type=rule_type,
            is_active=True
        ).first()

        if rule:
            result = rule.rule_value if isinstance(rule.rule_value, str) else str(rule.rule_value)
            ConfigService.set(cache_key, result)
            return result

        ConfigService.set(cache_key, default)
        return default
