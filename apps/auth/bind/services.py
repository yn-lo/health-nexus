import random
import string
from typing import Tuple
from django.conf import settings
from django.core.cache import cache
from django.db import models, transaction, DatabaseError

from .models import PatientDoctorBinding
from apps.auth.models import generate_qr_image

BIND_CODE_LENGTH = 6
BIND_CODE_CHARS = string.digits + string.ascii_uppercase  # 0-9 + A-Z (36 chars)


class BindingService:

    @staticmethod
    def _generate_bind_code() -> str:
        """Generate a random 6-char alphanumeric bind code."""
        return ''.join(random.choices(BIND_CODE_CHARS, k=BIND_CODE_LENGTH))

    @classmethod
    def _generate_unique_bind_code(cls, max_retries: int = 10) -> str:
        """Generate a unique bind code, retrying on collision."""
        from apps.auth.models import UserProfile
        for _ in range(max_retries):
            code = cls._generate_bind_code()
            if not UserProfile.objects.filter(bind_qr_code=code).exists():
                return code
        raise ValueError("无法生成唯一绑定码，请稍后重试")

    def generate_user_qr(self, user_id: int) -> Tuple[str, str]:
        """Generate QR code for a user (fixed, stored in user profile)"""
        from apps.auth.models import UserProfile

        cache_key = f'user_qr_{user_id}'
        cached = cache.get(cache_key)
        if cached:
            return cached
        user = UserProfile.objects.get(id=user_id)
        if not user.bind_qr_code:
            user.bind_qr_code = self._generate_unique_bind_code()
            user.save(update_fields=['bind_qr_code'])
        site_url = getattr(settings, 'SITE_URL', 'http://localhost:8000')
        qr_image = generate_qr_image(user.bind_qr_code, site_url)
        cache.set(cache_key, (user.bind_qr_code, qr_image), timeout=3600)
        return user.bind_qr_code, qr_image

    def _check_same_department(self, doctor, patient) -> bool:
        """检查医生和患者是否至少有一个共同科室"""
        from apps.base.models import UserDepartment
        doctor_dept_ids = set(
            UserDepartment.objects.filter(user=doctor)
            .values_list('department_id', flat=True)
        )
        patient_dept_ids = set(
            UserDepartment.objects.filter(user=patient)
            .values_list('department_id', flat=True)
        )
        return bool(doctor_dept_ids & patient_dept_ids)

    def create_binding_request(
        self, target_user_id: int, initiator_user_id: int, initiator_type: str
    ) -> Tuple[bool, str]:
        """
        Create a binding request.
        initiator_type: 'PATIENT' or 'DOCTOR'
        target_user: the user being scanned/requested to
        initiator_user: the user who initiated the request
        """
        from apps.auth.models import UserProfile
        target = UserProfile.objects.get(id=target_user_id)
        initiator = UserProfile.objects.get(id=initiator_user_id)

        if initiator_type == 'DOCTOR':
            if target.role not in [UserProfile.Role.PATIENT]:
                return False, "医护只能绑定患者"
            doctor_id = initiator_user_id
            patient_id = target_user_id
        else:
            if target.role not in [UserProfile.Role.DOCTOR, UserProfile.Role.NURSE]:
                return False, "患者只能绑定医护人员"
            doctor_id = target_user_id
            patient_id = initiator_user_id

        doctor = UserProfile.objects.get(id=doctor_id)
        patient = UserProfile.objects.get(id=patient_id)
        if not self._check_same_department(doctor, patient):
            return False, "只能绑定同科室的患者或医生"

        filter_kwargs = {
            'doctor_id': doctor_id,
            'patient_id': patient_id,
        }

        existing = PatientDoctorBinding.objects.filter(**filter_kwargs)

        if existing.filter(status=PatientDoctorBinding.Status.CONFIRMED).exists():
            return False, "您已经绑定过该用户"

        if existing.filter(status=PatientDoctorBinding.Status.PENDING).exists():
            return False, "已存在待确认的绑定请求"

        PatientDoctorBinding.objects.create(
            patient_id=patient_id,
            doctor_id=doctor_id,
            initiator=initiator_type,
            status=PatientDoctorBinding.Status.PENDING,
        )
        return True, "绑定请求已发送，等待对方确认"

    def confirm_binding(self, binding_id: int, user_id: int) -> Tuple[bool, str]:
        """Confirm a pending binding request with permission check"""
        try:
            with transaction.atomic():
                binding = PatientDoctorBinding.objects.select_for_update().get(
                    id=binding_id,
                    status=PatientDoctorBinding.Status.PENDING,
                )
        except PatientDoctorBinding.DoesNotExist:
            return False, "绑定请求不存在或已处理"
        except DatabaseError:
            return False, "系统繁忙，请稍后重试"

        if user_id not in [binding.patient_id, binding.doctor_id]:
            return False, "无权限操作此绑定请求"

        binding.confirm()
        return True, "绑定成功"

    def reject_binding(self, binding_id: int, user_id: int) -> Tuple[bool, str]:
        """Reject a pending binding request with permission check"""
        try:
            with transaction.atomic():
                binding = PatientDoctorBinding.objects.select_for_update().get(
                    id=binding_id,
                    status=PatientDoctorBinding.Status.PENDING,
                )
        except PatientDoctorBinding.DoesNotExist:
            return False, "绑定请求不存在或已处理"
        except DatabaseError:
            return False, "系统繁忙，请稍后重试"

        if user_id not in [binding.patient_id, binding.doctor_id]:
            return False, "无权限操作此绑定请求"

        binding.reject()
        return True, "已拒绝绑定请求"

    def revoke_binding(self, binding_id: int, user_id: int) -> Tuple[bool, str]:
        """Revoke a confirmed binding with permission check"""
        try:
            with transaction.atomic():
                binding = PatientDoctorBinding.objects.select_for_update().get(
                    id=binding_id,
                    status=PatientDoctorBinding.Status.CONFIRMED,
                )
        except PatientDoctorBinding.DoesNotExist:
            return False, "绑定请求不存在或已处理，只能解绑已确认的绑定"
        except DatabaseError:
            return False, "系统繁忙，请稍后重试"

        if user_id not in [binding.patient_id, binding.doctor_id]:
            return False, "无权限操作此绑定请求"

        binding.revoke()
        return True, "绑定已解除"

    def get_pending_requests_for_user(self, user_id: int):
        """Get all pending binding requests for a user"""
        return list(PatientDoctorBinding.objects.filter(
            models.Q(patient_id=user_id) | models.Q(doctor_id=user_id),
            status=PatientDoctorBinding.Status.PENDING,
        ).select_related('patient', 'doctor').order_by('-requested_at'))

    def get_confirmed_bindings_for_doctor(self, doctor_id: int):
        """Get all confirmed patient bindings for a doctor"""
        return list(PatientDoctorBinding.objects.filter(
            doctor_id=doctor_id,
            status=PatientDoctorBinding.Status.CONFIRMED,
        ).select_related('patient').order_by('-confirmed_at'))

    def get_confirmed_bindings_for_patient(self, patient_id: int):
        """Get all confirmed doctor bindings for a patient"""
        return list(PatientDoctorBinding.objects.filter(
            patient_id=patient_id,
            status=PatientDoctorBinding.Status.CONFIRMED,
        ).select_related('doctor').order_by('-confirmed_at'))

    def resolve_user_by_qr(self, qr_code: str):
        """Resolve a user by their bind_qr_code, returning extended info"""
        from apps.auth.models import UserProfile
        from apps.care.models import PatientProfile
        try:
            user = UserProfile.objects.get(bind_qr_code=qr_code.upper(), is_active=True)
            info = {
                'id': user.id,
                'username': user.username,
                'role': user.role,
                'role_display': user.get_role_display(),
                'avatar_url': user.avatar.url if user.avatar else None,
            }

            if user.role == UserProfile.Role.PATIENT:
                try:
                    profile = user.patient_profile
                    info['name'] = profile.name
                    info['gender'] = profile.gender
                    info['age'] = profile.age
                except PatientProfile.DoesNotExist:
                    info['name'] = user.username
                    info['gender'] = ''
                    info['age'] = 0
            else:
                info['name'] = user.username
                info['gender'] = ''
                info['age'] = 0

            return info
        except UserProfile.DoesNotExist:
            return None
