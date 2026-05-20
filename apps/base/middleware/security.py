"""Security headers middleware."""

from django.conf import settings


class SecurityHeadersMiddleware:
    def __init__(self, get_response):
        self.get_response = get_response
        self._allowed_origins = None

    def __call__(self, request):
        response = self.get_response(request)
        response['X-Content-Type-Options'] = 'nosniff'
        response['X-Frame-Options'] = 'SAMEORIGIN'
        response['X-XSS-Protection'] = '1; mode=block'
        response['Referrer-Policy'] = 'strict-origin-when-cross-origin'

        if not settings.DEBUG:
            response['Strict-Transport-Security'] = 'max-age=31536000; includeSubDomains'

        if 'Server' in response:
            del response['Server']

        origin = request.META.get('HTTP_ORIGIN', '')
        if origin:
            allowed_origin = self._get_allowed_origin(request, origin)
            if allowed_origin:
                response['Access-Control-Allow-Origin'] = allowed_origin
                response['Access-Control-Allow-Methods'] = 'GET, POST, PUT, DELETE, OPTIONS'
                response['Access-Control-Allow-Headers'] = 'Content-Type, Authorization, X-Requested-With'
                if allowed_origin != '*':
                    response['Access-Control-Allow-Credentials'] = 'true'
        return response

    def _get_allowed_origin(self, request, origin):
        if self._allowed_origins is None:
            self._allowed_origins = getattr(settings, 'CORS_ALLOWED_ORIGINS', [])
        if not self._allowed_origins:
            return '*'
        if origin in self._allowed_origins:
            return origin
        return None
