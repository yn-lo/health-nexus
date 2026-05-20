from typing import List, Optional
from django.core.exceptions import ValidationError
from django.db.models import Q
from apps.base.models import Department
from apps.base.pagination import PaginatedResult
from apps.care.models import PatientProfile


def get_patient_profile_by_user(user) -> Optional[PatientProfile]:
    try:
        return user.patient_profile
    except PatientProfile.DoesNotExist:
        return None


def get_patients_by_department(department_id: int) -> List[PatientProfile]:
    from apps.base.middleware.data_isolation import DepartmentDataIsolationMixin

    accessible_dept_ids = _get_accessible_department_ids(department_id)
    if not accessible_dept_ids:
        return []

    return list(
        PatientProfile.objects.filter(departments__id__in=accessible_dept_ids)
        .distinct()
    )


def get_patients_by_department_paginated(department_id: int, page=1, page_size=20) -> PaginatedResult:
    accessible_dept_ids = _get_accessible_department_ids(department_id)
    if not accessible_dept_ids:
        return PaginatedResult(items=[], total_count=0, page=page, page_size=page_size)

    qs = PatientProfile.objects.filter(departments__id__in=accessible_dept_ids).distinct()
    total_count = qs.count()
    offset = (page - 1) * page_size
    items = list(qs[offset:offset + page_size])
    return PaginatedResult(items=items, total_count=total_count, page=page, page_size=page_size)


def search_patients(query: str, department_id: int = None, page=1, page_size=20) -> PaginatedResult:
    if not query or len(query) < 2:
        return PaginatedResult(items=[], total_count=0, page=page, page_size=page_size)

    qs = PatientProfile.objects.filter(
        Q(name__icontains=query) | Q(user__phone__icontains=query)
    ).distinct()

    if department_id:
        accessible_ids = _get_accessible_department_ids(department_id)
        qs = qs.filter(departments__id__in=accessible_ids)

    total_count = qs.count()
    offset = (page - 1) * page_size
    items = list(qs[offset:offset + page_size])
    return PaginatedResult(items=items, total_count=total_count, page=page, page_size=page_size)


def _get_accessible_department_ids(department_id: int) -> List[int]:
    accessible = {department_id}
    current_id = department_id

    while True:
        parent_id = Department.objects.filter(id=current_id).values_list('parent_id', flat=True).first()
        if not parent_id or parent_id in accessible:
            break
        accessible.add(parent_id)
        current_id = parent_id

    return list(accessible)


def get_valid_department_ids(department_ids: List[int]) -> List[int]:
    return list(Department.objects.filter(id__in=department_ids).values_list('id', flat=True))


def get_department_by_id(dept_id: int) -> Optional[Department]:
    try:
        return Department.objects.get(id=dept_id)
    except Department.DoesNotExist:
        return None
