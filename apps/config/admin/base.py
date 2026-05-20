"""Admin mixin to track user for config audit logging."""


class AuditTrackingAdminMixin:
    """Mixin for ModelAdmin to track the user performing config changes."""

    def save_model(self, request, obj, form, change):
        if hasattr(request, 'user') and hasattr(request.user, 'get_username'):
            obj._audit_user = request.user.get_username()
        super().save_model(request, obj, form, change)
