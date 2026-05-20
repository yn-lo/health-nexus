"""Request timing and observability middleware for Team D platform governance."""
import logging
import time
from django.db import connection
from django.conf import settings

logger = logging.getLogger("health_nexus.observability")
slow_query_logger = logging.getLogger("health_nexus.slow_queries")

SLOW_QUERY_THRESHOLD_MS = getattr(settings, "SLOW_QUERY_THRESHOLD_MS", 200)
SLOW_REQUEST_THRESHOLD_MS = getattr(settings, "SLOW_REQUEST_THRESHOLD_MS", 1000)


class ObservabilityMiddleware:
    """Request timing, slow query detection, and observability middleware."""

    SKIP_PATHS_PREFIXES = ("/static/", "/media/", "/favicon.ico", "/health/")

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        if any(request.path.startswith(p) for p in self.SKIP_PATHS_PREFIXES):
            return self.get_response(request)

        start_time = time.time()
        initial_query_count = len(connection.queries)

        response = self.get_response(request)

        duration_ms = (time.time() - start_time) * 1000
        query_count = len(connection.queries) - initial_query_count

        if duration_ms >= SLOW_REQUEST_THRESHOLD_MS:
            logger.warning(
                "SLOW_REQUEST | method=%s | path=%s | status=%d | duration_ms=%.0f | queries=%d",
                request.method, request.path, response.status_code, duration_ms, query_count,
            )
        else:
            logger.info(
                "REQUEST | method=%s | path=%s | status=%d | duration_ms=%.0f | queries=%d",
                request.method, request.path, response.status_code, duration_ms, query_count,
            )

        if connection.queries:
            for q in connection.queries[initial_query_count:]:
                sql = q.get("sql", "")
                q_time = float(q.get("time", 0)) * 1000
                if q_time >= SLOW_QUERY_THRESHOLD_MS:
                    slow_query_logger.warning(
                        "SLOW_QUERY | path=%s | duration_ms=%.0f | sql=%s",
                        request.path, q_time, sql[:300],
                    )

        response["X-Request-Duration-Ms"] = str(int(duration_ms))
        return response
