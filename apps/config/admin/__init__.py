"""
Admin module for config app.

This file exists to trigger Django's admin autodiscovery.
All admin classes are defined in sub-modules under admin/.
"""
from apps.config.admin import (
    ai_provider,
    sensitive_word,
    safety_rule,
    rate_limit,
    rag,
    system,
    brand,
    audit_log,
    prompt_template,
)  # noqa: F401
