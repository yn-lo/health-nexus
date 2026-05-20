"""Audit logging helper for config changes."""
from apps.config.models import ConfigAuditLog


SENSITIVE_FIELD_PATTERNS = ['api_key', 'key', 'password', 'secret', 'token']
MASKED_VALUE = '****'


def _is_sensitive_field(field_name: str) -> bool:
    """Check if a field name indicates sensitive data."""
    return any(pattern in field_name.lower() for pattern in SENSITIVE_FIELD_PATTERNS)


def _mask_value(value: str) -> str:
    """Mask a sensitive value, showing first 2 and last 4 characters."""
    if not value or len(value) < 8:
        return MASKED_VALUE
    return value[:2] + MASKED_VALUE + value[-4:]


def _mask_sensitive_fields(data: dict, instance) -> dict:
    """Mask sensitive fields in a dictionary of field values."""
    masked = {}
    for field_name, field_value in data.items():
        if _is_sensitive_field(field_name):
            masked[field_name] = _mask_value(str(field_value))
        else:
            masked[field_name] = field_value
    return masked


def _get_model_identity_fields(instance) -> list:
    """Get fields that uniquely identify the config instance for display."""
    priority_fields = ['name', 'config_key', 'key', 'word', 'rule_key']
    for field in priority_fields:
        if hasattr(instance, field):
            return [field]
    return ['pk']


def log_config_change(
    instance,
    action: str,
    old_values: dict = None,
    new_values: dict = None,
    performed_by: str = 'system',
):
    """
    Log a configuration change to the audit log.

    Args:
        instance: The config model instance being changed.
        action: CREATE, UPDATE, or DELETE.
        old_values: Field values before the change (will be masked).
        new_values: Field values after the change (will be masked).
        performed_by: Username of the person making the change.
    """
    identity_fields = _get_model_identity_fields(instance)
    target_parts = [str(getattr(instance, f, 'unknown')) for f in identity_fields]
    config_target = f"{instance._meta.model_name}: {' - '.join(target_parts)}"

    ConfigAuditLog.objects.create(
        action_type=action,
        config_model=instance.__class__.__name__,
        config_target=config_target,
        performed_by=performed_by,
        old_values=_mask_sensitive_fields(old_values or {}, instance),
        new_values=_mask_sensitive_fields(new_values or {}, instance),
    )


def get_changed_fields(old_instance, new_instance) -> dict:
    """
    Compare two model instances and return a dict of changed fields.

    Returns dict with old and new values for each changed field.
    """
    changed = {}
    for field in new_instance._meta.fields:
        field_name = field.name
        if field_name in ('id', 'created_at', 'updated_at'):
            continue
        old_val = getattr(old_instance, field_name, None)
        new_val = getattr(new_instance, field_name, None)
        if old_val != new_val:
            changed[field_name] = {'old': old_val, 'new': new_val}
    return changed
