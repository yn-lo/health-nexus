"""Session security utilities for OWASP-compliant session management."""
import logging
from django.contrib.sessions.backends.db import SessionStore

logger = logging.getLogger(__name__)


def destroy_old_session(old_session_key: str) -> None:
    """销毁旧的 session 数据（OWASP Session Management）"""
    if not old_session_key:
        return
    try:
        old_session = SessionStore(session_key=old_session_key)
        if old_session.exists(old_session_key):
            old_session.delete()
    except Exception:
        logger.warning("Failed to destroy old session: %s", old_session_key)


def clear_session_before_login(request) -> None:
    """登录前刷新 session（CWE-384 Session Fixation 防护）"""
    request.session.flush()
