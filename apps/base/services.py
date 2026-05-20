from typing import List, Dict, Any, Optional
from django.db import transaction
import logging
from apps.base.models import Department, UserDepartment, DepartmentAuditLog, Notification
from apps.auth.models import UserProfile

logger = logging.getLogger(__name__)


class AuditLogService:
    def log_action(self, department, user, action_type, target='', details=None):
        try:
            DepartmentAuditLog.objects.create(
                department=department,
                performed_by=user,
                action_type=action_type,
                target=target,
                details=details or {}
            )
        except Exception as e:
            logger.error(f"Failed to create audit log: {e}")

    def get_department_logs(self, department_id, limit=50):
        return list(
            DepartmentAuditLog.objects
            .filter(department_id=department_id)
            .order_by('-created_at')[:limit]
        )


class DepartmentService:
    def __init__(self):
        self.audit_service = AuditLogService()

    def get_public_departments(self) -> List[Department]:
        return list(Department.objects.filter(is_public=True))

    def get_user_departments(self, user) -> List[Department]:
        if not user or not user.is_authenticated:
            return self.get_public_departments()

        public_depts = Department.objects.filter(is_public=True)

        if user.role == UserProfile.Role.PATIENT:
            if hasattr(user, 'patient_profile'):
                patient_depts = user.patient_profile.departments.all()
                all_dept_ids = set(
                    list(public_depts.values_list('id', flat=True))
                    + list(patient_depts.values_list('id', flat=True))
                )
                return list(Department.objects.filter(id__in=all_dept_ids))

        m2m_dept_ids = UserDepartment.objects.filter(
            user=user
        ).values_list('department_id', flat=True)

        if m2m_dept_ids:
            all_dept_ids = set(
                list(public_depts.values_list('id', flat=True)) + list(m2m_dept_ids)
            )
            return list(Department.objects.filter(id__in=all_dept_ids))

        return list(public_depts)

    def get_all_departments(self, exclude_public: bool = False) -> List[Department]:
        qs = Department.objects.all()
        if exclude_public:
            qs = qs.filter(is_public=False)
        return list(qs)

    @transaction.atomic
    def create_department(self, data: Dict[str, Any], user=None) -> Department:
        name = data.get('name')
        if not name:
            raise ValueError("name is required")

        if Department.objects.filter(name=name).exists():
            raise ValueError("科室名称已存在")

        dept = Department.objects.create(
            name=name,
            tenant_code=data.get('tenant_code', ''),
            parent=data.get('parent'),
            manager=data.get('manager'),
            description=data.get('description', ''),
            config=data.get('config', {}),
        )
        audit_data = {k: v for k, v in data.items()}
        if 'parent' in audit_data:
            audit_data['parent'] = audit_data.get('parent_id') or getattr(audit_data.get('parent'), 'id', None)
        if 'manager' in audit_data:
            audit_data['manager'] = audit_data.get('manager_id') or getattr(audit_data.get('manager'), 'id', None)
        self.audit_service.log_action(
            department=dept,
            user=user,
            action_type=DepartmentAuditLog.Action.CREATE,
            target=name,
            details={'data': audit_data}
        )
        return dept

    @transaction.atomic
    def update_department(self, dept_id: int, data: Dict[str, Any], user=None) -> Department:
        dept = Department.objects.get(id=dept_id)

        if 'name' in data and data['name'] != dept.name:
            if Department.objects.filter(name=data['name']).exclude(id=dept_id).exists():
                raise ValueError("科室名称已存在")

        if 'is_active' in data:
            new_active = data['is_active']
            if new_active and not dept.is_active:
                parent = dept.parent
                while parent:
                    if not parent.is_active:
                        raise ValueError(f"父科室'{parent.name}'未启用")
                    parent = parent.parent
            elif not new_active and dept.is_active:
                descendant_ids = self._get_descendant_ids(dept)
                if descendant_ids:
                    Department.objects.filter(id__in=descendant_ids).update(is_active=False)

        changes = {}
        for field in ['name', 'description', 'tenant_code', 'config', 'is_public', 'is_active']:
            if field in data:
                changes[field] = {'old': getattr(dept, field), 'new': data[field]}
                setattr(dept, field, data[field])
        if 'parent' in data:
            changes['parent'] = {'old': dept.parent_id, 'new': data['parent'].id if data['parent'] else None}
            dept.parent = data['parent']
        if 'manager' in data:
            changes['manager'] = {'old': dept.manager_id, 'new': data['manager'].id if data['manager'] else None}
            dept.manager = data['manager']
        dept.save()
        self.audit_service.log_action(
            department=dept,
            user=user,
            action_type=DepartmentAuditLog.Action.UPDATE,
            target=data.get('name', dept.name),
            details={'changes': changes}
        )
        return dept

    def _get_descendant_ids(self, dept: Department) -> List[int]:
        ids = []
        children = list(dept.children.all())
        while children:
            child = children.pop(0)
            ids.append(child.id)
            children.extend(list(child.children.all()))
        return ids

    @transaction.atomic
    def delete_department(self, dept_id: int, user=None) -> bool:
        dept = Department.objects.filter(id=dept_id).first()
        if not dept:
            return False

        if dept.children.exists():
            raise ValueError("不能删除有子科室的科室")

        if dept.user_departments.exists():
            raise ValueError("不能删除有成员的科室")

        dept_name = dept.name
        dept.delete()
        self.audit_service.log_action(
            department=None,
            user=user,
            action_type=DepartmentAuditLog.Action.DELETE,
            target=dept_name
        )
        return True

    def get_department_tree(self) -> List[Dict[str, Any]]:
        all_depts = list(Department.objects.all().prefetch_related('children'))
        dept_map = {}
        for dept in all_depts:
            dept_map[dept.id] = {
                'id': dept.id,
                'name': dept.name,
                'tenant_code': dept.tenant_code,
                'description': dept.description,
                'is_public': dept.is_public,
                'children': [],
            }
        roots = []
        for dept in all_depts:
            node = dept_map[dept.id]
            if dept.parent_id is None:
                roots.append(node)
            else:
                parent_node = dept_map.get(dept.parent_id)
                if parent_node:
                    parent_node['children'].append(node)
        return roots

    @transaction.atomic
    def add_member(self, dept_id: int, user_id: int, role: str, is_primary: bool = False, actor=None) -> 'UserDepartment':
        ud = UserDepartment.objects.create(
            department_id=dept_id,
            user_id=user_id,
            role=role,
            is_primary=is_primary,
        )
        dept = Department.objects.get(id=dept_id)
        member_user = UserProfile.objects.get(id=user_id)
        self.audit_service.log_action(
            department=dept,
            user=actor,
            action_type=DepartmentAuditLog.Action.ADD_MEMBER,
            target=f'{member_user.username}',
            details={'role': role, 'is_primary': is_primary}
        )
        return ud

    def remove_member(self, dept_id: int, user_id: int, actor=None) -> bool:
        ud = UserDepartment.objects.filter(
            department_id=dept_id, user_id=user_id
        ).first()
        deleted = 0
        if ud:
            member_user = ud.user
            deleted = 1
            dept = Department.objects.get(id=dept_id)
            self.audit_service.log_action(
                department=dept,
                user=actor,
                action_type=DepartmentAuditLog.Action.REMOVE_MEMBER,
                target=f'{member_user.username}',
                details={'role': ud.role}
            )
        UserDepartment.objects.filter(
            department_id=dept_id, user_id=user_id
        ).delete()
        return deleted > 0

    def get_members(self, dept_id: int) -> List[Dict[str, Any]]:
        user_depts = UserDepartment.objects.filter(
            department_id=dept_id
        ).select_related('user')
        return [
            {
                'id': ud.id,
                'user_id': ud.user.id,
                'username': ud.user.username,
                'role': ud.role,
                'is_primary': ud.is_primary,
            }
            for ud in user_depts
        ]


class NotificationService:
    def notify(
        self,
        recipient: UserProfile,
        category: str,
        title: str,
        content: str = '',
        related_url: str = '',
        source_id: str = '',
    ) -> Notification:
        return Notification.objects.create(
            recipient=recipient,
            category=category,
            title=title,
            content=content,
            related_url=related_url,
            source_id=source_id,
        )

    def notify_department_staff(
        self,
        department: Department,
        category: str,
        title: str,
        content: str = '',
        related_url: str = '',
        source_id: str = '',
    ) -> int:
        staff_ids = UserDepartment.objects.filter(
            department=department,
        ).values_list('user_id', flat=True)

        notifications = [
            Notification(
                recipient_id=uid,
                category=category,
                title=title,
                content=content,
                related_url=related_url,
                source_id=source_id,
            )
            for uid in staff_ids
        ]
        Notification.objects.bulk_create(notifications)
        return len(notifications)

    def get_unread_count(self, user: UserProfile) -> int:
        return Notification.objects.filter(recipient=user, is_read=False).count()

    def mark_as_read(self, notification_id: int, user: UserProfile) -> bool:
        updated = Notification.objects.filter(
            id=notification_id, recipient=user, is_read=False
        ).update(is_read=True)
        return updated > 0

    def mark_all_as_read(self, user: UserProfile) -> int:
        return Notification.objects.filter(
            recipient=user, is_read=False
        ).update(is_read=True)


