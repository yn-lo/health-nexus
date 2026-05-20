"""Audit logging utilities for tracking data access and user activities."""
import logging

audit_logger = logging.getLogger("health_nexus.audit")


class AuditLogger:
    """Simple audit logger using Python's standard logging."""

    def log_login(self, user_id: int, username: str, ip: str, success: bool):
        audit_logger.info(
            "LOGIN | user_id=%s | username=%s | ip=%s | success=%s",
            user_id, username, ip, success
        )

    def log_login_failure(self, username: str, ip: str, reason: str):
        audit_logger.warning(
            "LOGIN_FAILURE | username=%s | ip=%s | reason=%s",
            username, ip, reason
        )

    def log_data_access(
        self,
        user_id: int,
        username: str,
        data_type: str,
        target_id: str,
        department: str = "",
        ip: str = "",
        action: str = "view",
    ):
        audit_logger.info(
            "DATA_ACCESS | user_id=%s | username=%s | type=%s | target=%s | dept=%s | ip=%s | action=%s",
            user_id, username, data_type, target_id, department, ip, action
        )


def get_audit_logger() -> AuditLogger:
    """Get an AuditLogger instance."""
    return AuditLogger()
