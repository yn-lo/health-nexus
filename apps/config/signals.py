from django.db.models.signals import post_save, post_delete, pre_save
from django.apps import apps
from django.core.exceptions import ObjectDoesNotExist
from apps.config.models.base import BaseConfig
from apps.config.audit_logger import log_config_change


_config_pre_state = {}


def _config_pre_save(sender, instance, **kwargs):
    """Capture the state of a config instance before it's saved."""
    obj_id = id(instance)

    if instance.pk:
        try:
            old_instance = sender.objects.get(pk=instance.pk)
            _config_pre_state[obj_id] = {
                'instance_pk': instance.pk,
                'old_values': {
                    field.name: getattr(old_instance, field.name)
                    for field in sender._meta.fields
                    if field.name not in ('id', 'created_at', 'updated_at')
                },
                'is_update': True,
            }
        except ObjectDoesNotExist:
            _config_pre_state[obj_id] = {
                'instance_pk': instance.pk,
                'old_values': {},
                'is_update': False,
            }
    else:
        _config_pre_state[obj_id] = {
            'instance_pk': None,
            'old_values': {},
            'is_update': False,
        }


def _config_saved(sender, instance, **kwargs):
    """Invalidate cache and log audit when config is saved."""
    instance.invalidate_cache()
    _log_config_audit(sender, instance)


def _config_deleted(sender, instance, **kwargs):
    """Invalidate cache and log audit when config is deleted."""
    instance.invalidate_cache()
    log_config_change(
        instance,
        action='DELETE',
        old_values={
            field.name: getattr(instance, field.name)
            for field in sender._meta.fields
            if field.name not in ('id', 'created_at', 'updated_at')
        },
    )


def _log_config_audit(sender, instance):
    """Create an audit log entry for config changes."""
    obj_id = id(instance)
    state = _config_pre_state.pop(obj_id, None)
    if not state:
        return

    action = 'UPDATE' if state['is_update'] else 'CREATE'

    old_values = state['old_values']

    new_values = {
        field.name: getattr(instance, field.name)
        for field in sender._meta.fields
        if field.name not in ('id', 'created_at', 'updated_at')
    }

    performed_by = getattr(instance, '_audit_user', 'system')

    log_config_change(
        instance,
        action=action,
        old_values=old_values,
        new_values=new_values,
        performed_by=performed_by,
    )


def connect_config_signals():
    """Connect signals to all concrete subclasses of BaseConfig."""
    for model in apps.get_models():
        try:
            if issubclass(model, BaseConfig) and not model._meta.abstract:
                pre_save.connect(_config_pre_save, sender=model)
                post_save.connect(_config_saved, sender=model)
                post_delete.connect(_config_deleted, sender=model)
        except TypeError:
            pass


connect_config_signals()
