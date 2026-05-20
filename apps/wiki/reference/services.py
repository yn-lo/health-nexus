from typing import List, Optional
from datetime import datetime
from django.db.models import QuerySet
from django.utils import timezone
from apps.wiki.reference.models import ArticleReference, ArticleReferenceStatus


class ArticleReferenceService:
    def __init__(self):
        pass

    def _resolve_article_and_department(
        self, article_id: int, department_id: int
    ) -> tuple:
        from apps.wiki.models import Article
        from apps.base.models import Department

        try:
            article = Article.objects.get(id=article_id)
        except Article.DoesNotExist:
            return None, None, "文章不存在"

        try:
            dept = Department.objects.get(id=department_id)
        except Department.DoesNotExist:
            return None, None, "科室不存在"

        return article, dept, None

    def reference_article(
        self,
        article_id: int,
        target_department_id: int,
        user=None
    ) -> tuple[bool, str]:
        article, dept, error = self._resolve_article_and_department(
            article_id, target_department_id
        )
        if error:
            return False, error

        if not getattr(article, 'allow_reference', True):
            return False, "该文章不允许引用"

        if user and not self._can_user_request_reference(user, dept):
            return False, "无权申请此文章引用"

        ref, created = ArticleReference.objects.get_or_create(
            source_article=article,
            target_department=dept,
            defaults={'status': ArticleReferenceStatus.PENDING}
        )

        if not created and ref.status == ArticleReferenceStatus.PENDING:
            return False, "已存在待审核的引用申请"

        return True, "引用申请已提交" if created else "已存在引用"

    def approve_reference(
        self,
        article_id: int,
        department_id: int,
        user=None
    ) -> tuple[bool, str]:
        article, dept, error = self._resolve_article_and_department(
            article_id, department_id
        )
        if error:
            return False, error

        if user and not self._can_user_approve_reference(user, dept):
            return False, "无权审核此引用"

        try:
            ref = ArticleReference.objects.get(
                source_article=article,
                target_department=dept
            )
        except ArticleReference.DoesNotExist:
            return False, "引用申请不存在"

        ref.status = ArticleReferenceStatus.APPROVED
        ref.authorized_by = user
        ref.approved_by_department = dept
        ref.authorized_at = timezone.now()
        ref.reason = ''
        ref.save(update_fields=['status', 'authorized_by', 'approved_by_department', 'authorized_at', 'reason'])

        return True, "引用授权成功"

    def reject_reference(
        self,
        article_id: int,
        department_id: int,
        reason: str,
        user=None
    ) -> tuple[bool, str]:
        article, dept, error = self._resolve_article_and_department(
            article_id, department_id
        )
        if error:
            return False, error

        if user and not self._can_user_approve_reference(user, dept):
            return False, "无权审核此引用"

        try:
            ref = ArticleReference.objects.get(
                source_article=article,
                target_department=dept
            )
        except ArticleReference.DoesNotExist:
            return False, "引用申请不存在"

        ref.status = ArticleReferenceStatus.REJECTED
        ref.authorized_by = user
        ref.authorized_at = timezone.now()
        ref.reason = reason
        ref.save(update_fields=['status', 'authorized_by', 'authorized_at', 'reason'])

        return True, "引用已拒绝"

    def get_referenced_articles(self, department_id: int) -> QuerySet:
        from apps.wiki.models import Article
        return Article.objects.filter(
            references_to__target_department_id=department_id,
            references_to__status=ArticleReferenceStatus.APPROVED,
            status=Article.Status.PUBLISHED,
            is_deleted=False,
        )

    def get_pending_references(self, department_id: int) -> QuerySet:
        from apps.wiki.models import Article
        return Article.objects.filter(
            references_to__target_department_id=department_id,
            references_to__status=ArticleReferenceStatus.PENDING,
            status=Article.Status.PUBLISHED,
        )

    def get_reference_status(
        self,
        article_id: int,
        department_id: int
    ) -> Optional[str]:
        try:
            ref = ArticleReference.objects.get(
                source_article_id=article_id,
                target_department_id=department_id
            )
            return ref.status
        except ArticleReference.DoesNotExist:
            return None

    def get_article_reference_count(self, article_id: int) -> int:
        return ArticleReference.objects.filter(source_article_id=article_id).count()

    def remove_reference(self, article_id: int, department_id: int, user=None) -> tuple[bool, str]:
        return self.revoke_reference(article_id, department_id, user)

    def get_available_public_articles(self, department_id: int) -> QuerySet:
        from apps.wiki.models import Article
        from apps.base.models import Department

        dept = Department.objects.get(id=department_id)
        parent_ids = self._get_public_parent_dept_ids(dept)

        return Article.objects.filter(
            department_id__in=parent_ids,
            status=Article.Status.PUBLISHED,
            allow_reference=True
        ).exclude(
            references_to__target_department_id=department_id
        )

    def _get_public_parent_dept_ids(self, dept) -> List[int]:
        ids = []
        current = dept
        while current and current.is_public:
            ids.append(current.id)
            current = current.parent

        return ids

    def _can_user_manage_department(self, user, dept) -> bool:
        if user and getattr(user, 'is_superuser', False):
            return True
        from apps.base.models import UserDepartment
        return UserDepartment.objects.filter(
            user=user,
            department=dept,
            role=UserDepartment.Role.ADMIN
        ).exists()

    _can_user_request_reference = _can_user_manage_department
    _can_user_approve_reference = _can_user_manage_department

    def revoke_reference(
        self,
        article_id: int,
        department_id: int,
        user=None
    ) -> tuple[bool, str]:
        from apps.wiki.models import Article
        from apps.base.models import Department

        try:
            article = Article.objects.get(id=article_id)
        except Article.DoesNotExist:
            return False, "文章不存在"

        try:
            dept = Department.objects.get(id=department_id)
        except Department.DoesNotExist:
            return False, "科室不存在"

        if user and not self._can_user_manage_department(user, dept):
            return False, "无权撤销此引用"

        try:
            ref = ArticleReference.objects.get(
                source_article=article,
                target_department=dept
            )
        except ArticleReference.DoesNotExist:
            return False, "引用不存在"

        if ref.status != ArticleReferenceStatus.APPROVED:
            return False, "只能撤销已授权的引用"

        return ref.revoke(user)
