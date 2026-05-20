import hashlib
from typing import List, Optional
from django.contrib.auth import get_user_model
from apps.base.models import Department

User = get_user_model()


class UserService:
    def get_user_by_id(self, user_id: int):
        try:
            return User.objects.get(id=user_id)
        except User.DoesNotExist:
            return None

    def get_user_by_phone(self, phone: str):
        if not phone:
            return None
        try:
            phone_hash = hashlib.sha256(phone.encode('utf-8')).hexdigest()
            return User.objects.get(phone_hash=phone_hash)
        except User.DoesNotExist:
            return None
        except Exception:
            return None

    def get_user_by_username(self, username: str):
        try:
            return User.objects.get(username=username)
        except User.DoesNotExist:
            return None

    def check_permission(self, user, action: str, resource_type: str, resource_id: int = None) -> bool:
        if not user or not user.is_authenticated:
            return False

        role = user.role

        if role == User.Role.SUPER_ADMIN:
            return True

        if role == User.Role.DEPT_ADMIN:
            if resource_type == 'department':
                return user.departments.filter(id=resource_id).exists()
            return True

        if user.is_medical_staff:
            if resource_type == 'department':
                return user.departments.filter(id=resource_id).exists()
            return True

        if role == User.Role.PATIENT:
            if resource_type == 'patient_profile':
                try:
                    return resource_id == user.patient_profile.id
                except Exception:
                    return False
            if resource_type == 'department':
                try:
                    return user.patient_profile.departments.filter(id=resource_id).exists()
                except Exception:
                    return False
            return False

        return False

    def get_users_by_department(self, department_id: int, role: str = None) -> List:
        qs = User.objects.filter(departments__id=department_id)
        if role:
            qs = qs.filter(role=role)
        return list(qs)


