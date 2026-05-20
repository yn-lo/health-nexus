from apps.config.services.config_service import ConfigService
from apps.config.models.rate_limit_rule import RateLimitRule
from apps.config.defaults import DEFAULT_RATE_LIMIT_RULES


def _build_defaults_by_name():
    result = {}
    for rule in DEFAULT_RATE_LIMIT_RULES:
        name = rule['name']
        if rule['rule_type'] == 'SMS':
            result[name] = {'limit': rule['limit'], 'window': rule['window']}
        elif rule['rule_type'] == 'ANONYMOUS_CHAT':
            result[name] = {'limit': rule['limit'], 'window': rule['window']}
        elif rule['rule_type'] == 'GLOBAL':
            result[name] = {'limit': rule['limit'], 'window': rule['window']}
    return result


_DEFAULTS_BY_NAME = _build_defaults_by_name()


class RateLimitConfigService:

    @classmethod
    def get_global_limits(cls, user_type: str) -> dict:
        cache_key = f"ratelimit:global:{user_type}"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rule_name = f"{user_type}_global"
        rule = RateLimitRule.objects.filter(
            rule_type='GLOBAL',
            name=rule_name,
            is_active=True
        ).first()

        if rule:
            result = {'limit': rule.limit, 'window': rule.window}
            ConfigService.set(cache_key, result)
            return result

        default = _DEFAULTS_BY_NAME.get(rule_name)
        if default:
            ConfigService.set(cache_key, default)
            return default

        return None

    @classmethod
    def get_all_path_limits(cls) -> dict:
        """返回所有路径限流规则，格式为 {rule_name: {path, methods, limit, window}}"""
        cache_key = "ratelimit:path:all"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rules = RateLimitRule.objects.filter(rule_type='PATH', is_active=True)
        result = {}
        for rule in rules:
            result[rule.name] = {
                'path': rule.path,
                'methods': rule.methods if rule.methods else ['GET', 'POST'],
                'limit': rule.limit,
                'window': rule.window,
            }

        if result:
            ConfigService.set(cache_key, result)

        return result

    @classmethod
    def get_sms_limits(cls) -> dict:
        cache_key = "ratelimit:sms"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rules = RateLimitRule.objects.filter(rule_type='SMS', is_active=True)
        result = {}
        for rule in rules:
            result[rule.name] = {'limit': rule.limit, 'window': rule.window}

        if not result:
            result = {name: data for name, data in _DEFAULTS_BY_NAME.items()}

        ConfigService.set(cache_key, result)
        return result

    @classmethod
    def get_anonymous_chat_limits(cls) -> dict:
        cache_key = "ratelimit:anonymous_chat"
        value = ConfigService.get(cache_key)
        if value is not None:
            return value

        rule = RateLimitRule.objects.filter(
            rule_type='ANONYMOUS_CHAT',
            name='anonymous_chat',
            is_active=True
        ).first()

        if rule:
            result = {'limit': rule.limit, 'window': rule.window}
            ConfigService.set(cache_key, result)
            return result

        return _DEFAULTS_BY_NAME.get('anonymous_chat', {'limit': 5, 'window': 86400})
