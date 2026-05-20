import hashlib
import json
import logging
from django.http import JsonResponse, HttpResponseRedirect, StreamingHttpResponse
from django.utils.deprecation import MiddlewareMixin

logger = logging.getLogger(__name__)

SKIP_RATE_LIMIT_PREFIXES = ('/static/', '/media/', '/error/', '/health/', '/accounts/terms/')
GLOBAL_LIMIT_EXEMPT_PREFIXES = ('/accounts/login/', '/accounts/register/', '/accounts/password-reset/', '/accounts/phone-login/', '/accounts/patient-login/', '/accounts/staff-login/', '/staff/login/')

PATH_ALIASES = {
    '/accounts/staff-login/': ['/staff/login/'],
}


class RateLimitMiddleware(MiddlewareMixin):
    def __init__(self, get_response):
        self.get_response = get_response
        self._path_strategy = None
        self._path_strategy_version = -1
        self._global_strategies = {}

    def _build_path_strategy(self):
        from apps.config.services.ratelimit_service import RateLimitConfigService
        from .strategies import PathBasedRateLimitStrategy

        db_limits = RateLimitConfigService.get_all_path_limits()
        if not db_limits:
            return PathBasedRateLimitStrategy({})

        path_limits = {}
        for rule_name, rule_data in db_limits.items():
            path = rule_data.get('path', '')
            methods = rule_data.get('methods', ['GET', 'POST'])
            limit = rule_data['limit']
            window = rule_data['window']
            aliases = PATH_ALIASES.get(path, [])

            for method in methods:
                key = f"{method}:{path}"
                path_limits[key] = {
                    'path': path,
                    'methods': [method],
                    'limit': limit,
                    'window': window,
                    'aliases': aliases,
                }

        return PathBasedRateLimitStrategy(path_limits)

    def _get_path_strategy(self):
        from django.core.cache import cache as django_cache
        current_version = django_cache.get("ratelimit:path_strategy_version", 0)
        if self._path_strategy is None or current_version != self._path_strategy_version:
            self._path_strategy = self._build_path_strategy()
            self._path_strategy_version = current_version
        return self._path_strategy

    def _get_global_strategy(self, user_type: str, limit: int, window: int):
        cache_key = f"{user_type}:{limit}:{window}"
        if cache_key not in self._global_strategies:
            from .strategies import FixedWindowRateLimitStrategy
            self._global_strategies[cache_key] = FixedWindowRateLimitStrategy(
                limit=limit,
                window_seconds=window,
            )
        return self._global_strategies[cache_key]

    def _get_identifier(self, request) -> str:
        if request.user.is_authenticated:
            return f"user:{request.user.id}"
        ip = request.META.get('REMOTE_ADDR', 'unknown')
        ua = request.META.get('HTTP_USER_AGENT', '')[:50]
        ua_hash = hashlib.md5(ua.encode()).hexdigest()[:8]
        return f"ip:{ip}:ua:{ua_hash}"

    def _get_user_type(self, request) -> str:
        if request.user.is_authenticated:
            return 'authenticated'
        return 'anonymous'

    def _is_ajax(self, request) -> bool:
        return request.headers.get('X-Requested-With') == 'XMLHttpRequest'

    def _should_skip_rate_limit(self, request) -> bool:
        for prefix in SKIP_RATE_LIMIT_PREFIXES:
            if request.path.startswith(prefix):
                return True
        return False

    def _is_global_limit_exempt(self, request) -> bool:
        if request.method != 'GET':
            return False
        for prefix in GLOBAL_LIMIT_EXEMPT_PREFIXES:
            if request.path.startswith(prefix):
                return True
        return False

    def _is_sse(self, request) -> bool:
        return request.headers.get('Accept') == 'text/event-stream'

    def _rate_limit_response(self, request, retry_after: int):
        if self._is_sse(request):
            return self._sse_rate_limit_response(retry_after)
        if self._is_ajax(request):
            return JsonResponse({
                'error': '请求过于频繁，请稍后再试',
                'retry_after': retry_after
            }, status=429)
        error_url = f'/error/429/?retry_after={retry_after}&path={request.path}'
        return HttpResponseRedirect(error_url)

    def _sse_rate_limit_response(self, retry_after: int):
        error_data = json.dumps({
            'type': 'error',
            'error_type': 'rate_limited',
            'message': '请求过于频繁，请稍后再试',
            'suggestion': f'请 {retry_after} 秒后再试',
        }, ensure_ascii=False)

        def stream():
            yield f'event: rate_limited\ndata: {json.dumps({"message": "请求过于频繁，请稍后再试", "retry_after": retry_after}, ensure_ascii=False)}\n\n'
            yield f'data: {error_data}\n\n'
            yield f'data: {json.dumps({"type": "complete"})}\n\n'

        response = StreamingHttpResponse(stream(), content_type='text/event-stream')
        response['Cache-Control'] = 'no-cache'
        response['X-Accel-Buffering'] = 'no'
        return response

    def __call__(self, request):
        return self.get_response(request)

    def process_view(self, request, view_func, view_args, view_kwargs):
        from django.conf import settings
        if not getattr(settings, 'RATE_LIMIT_ENABLED', True):
            return None

        if self._should_skip_rate_limit(request):
            return None

        try:
            return self._check_rate_limit(request)
        except Exception:
            logger.exception("Rate limit check failed, allowing request")
            return None

    def _check_rate_limit(self, request):
        from apps.config.services.ratelimit_service import RateLimitConfigService

        identifier = self._get_identifier(request)
        user_type = self._get_user_type(request)
        global_remaining = None

        if not self._is_global_limit_exempt(request):
            global_limits = RateLimitConfigService.get_global_limits(user_type)
            if global_limits is not None:
                global_strategy = self._get_global_strategy(
                    user_type, global_limits['limit'], global_limits['window']
                )
                global_allowed, global_remaining = global_strategy.is_allowed(identifier)

                if not global_allowed:
                    retry_after = global_strategy.get_retry_after(identifier)
                    return self._rate_limit_response(request, retry_after)

        path_strategy = self._get_path_strategy()
        allowed, remaining, retry_after = path_strategy.is_allowed(request.path, request.method, identifier)

        if not allowed:
            return self._rate_limit_response(request, retry_after)

        if global_remaining is not None:
            request._rate_limit_remaining = global_remaining

        return None

    def process_response(self, request, response):
        remaining = getattr(request, '_rate_limit_remaining', None)
        if remaining is not None:
            response['X-RateLimit-Remaining'] = str(remaining)
        return response
