from django.contrib.auth import get_user_model
from apps.base.logging.audit import get_audit_logger

User = get_user_model()


class StaffDashboardService:
    @staticmethod
    def get_dashboard_context(user, request=None):
        audit = get_audit_logger()
        ip = request.META.get('REMOTE_ADDR', 'unknown') if request else None
        audit.log_data_access(
            user_id=user.id,
            username=user.username,
            data_type='staff_dashboard',
            target_id=str(user.id),
            action='view',
            ip=ip
        )

        from apps.auth.bind.services import BindingService
        pending_bind_count = len(BindingService().get_pending_requests_for_user(user.id))

        from apps.wiki.models import Article
        pending_articles_count = Article.objects.filter(
            status=Article.Status.PENDING,
            is_deleted=False,
        ).count()

        from apps.auth.bind.models import PatientDoctorBinding
        bound_patients_count = PatientDoctorBinding.objects.filter(
            doctor=user,
            status=PatientDoctorBinding.Status.CONFIRMED,
        ).count()

        context = {
            'user': user,
            'user_role_display': dict(User.Role.choices).get(user.role, ''),
            'pending_bind_count': pending_bind_count,
            'pending_articles_count': pending_articles_count,
            'my_patients_count': bound_patients_count,
            'recent_bindings': PatientDoctorBinding.objects.filter(
                doctor=user,
                status=PatientDoctorBinding.Status.CONFIRMED,
            ).select_related('patient').order_by('-confirmed_at')[:5],
            'recent_pending_articles': Article.objects.filter(
                status=Article.Status.PENDING,
                is_deleted=False,
            ).select_related('author').order_by('-created_at')[:3],
        }

        if user.role in [User.Role.NURSE, User.Role.SUPER_ADMIN]:
            context['data_entry_tasks_count'] = 0

        if user.departments.exists():
            context['department'] = user.departments.first()

        return context
