from typing import List

from django.contrib.postgres.search import SearchQuery, SearchRank
from pgvector.django import CosineDistance

from apps.base.models import Department
from apps.auth.models import UserProfile
from apps.wiki.models import Article, ArticleChunk
from .embedding_client import EmbeddingClientStrategy, OpenAICompatibleEmbeddingClient
from .hybrid_search import reciprocal_rank_fusion
from .reranker import RerankerService


CANDIDATE_MULTIPLIER = 5


class KnowledgeSearchService:

    def __init__(self, embedding_client: EmbeddingClientStrategy = None, reranker: RerankerService = None):
        self._embedding_client = embedding_client or OpenAICompatibleEmbeddingClient()
        self._reranker = reranker or RerankerService()

    def _generate_embedding(self, text: str) -> List[float]:
        return self._embedding_client.embed(text)

    def search_similar_chunks(
        self,
        query: str,
        departments: List[Department] = None,
        top_k: int = 3,
        user=None,
    ) -> List[ArticleChunk]:
        try:
            query_vec = self._generate_embedding(query)
        except Exception:
            return []

        qs = self._build_filtered_qs(departments, user)
        if qs is None:
            return []

        candidate_k = top_k * CANDIDATE_MULTIPLIER

        vector_results = list(
            qs.annotate(
                distance=CosineDistance("embedding", query_vec)
            ).order_by("distance")[:candidate_k]
        )

        bm25_results = self._bm25_search(qs, query, candidate_k)

        if bm25_results:
            fused = reciprocal_rank_fusion(vector_results, bm25_results, top_k=candidate_k)
            candidates = [r.chunk for r in fused]
        else:
            candidates = vector_results

        if len(candidates) > top_k and len(candidates) > 3:
            if self._is_rerank_enabled():
                return self._reranker.rerank(query, candidates, top_k=top_k)

        return candidates

    @staticmethod
    def _is_rerank_enabled() -> bool:
        from apps.config.models import RAGConfig
        from apps.config.services.ai_service import AIConfigService
        try:
            config = RAGConfig.objects.get(config_key='rerank_enabled')
            if not int(config.config_value):
                return False
        except (RAGConfig.DoesNotExist, ValueError, TypeError):
            pass
        return AIConfigService.has_active_rerank_config()

    def _bm25_search(self, qs, query: str, limit: int) -> list:
        """使用 PostgreSQL 全文搜索进行 BM25 检索。"""
        try:
            search_query = SearchQuery(query, config='simple')
            ranked_qs = qs.filter(
                search_vector=search_query
            ).annotate(
                rank=SearchRank(search_vector, search_query)
            ).order_by('-rank')[:limit]

            return list(ranked_qs)
        except Exception:
            return []

    def _build_filtered_qs(self, departments, user):
        """构建带权限过滤的查询集。返回 None 表示无权限。"""
        from apps.wiki.reference.models import ArticleReference, ArticleReferenceStatus

        qs = ArticleChunk.objects.select_related('article').filter(
            article__status=Article.Status.PUBLISHED,
            is_active=True,
        )

        if departments:
            dept_ids = [d.id for d in departments]
            dept_ids_with_cited = set(dept_ids)
            for dept in departments:
                cited_article_ids = ArticleReference.objects.filter(
                    target_department_id=dept.id,
                    status=ArticleReferenceStatus.APPROVED
                ).values_list('source_article_id', flat=True)
                if cited_article_ids:
                    cited_dept_ids = Article.objects.filter(
                        id__in=cited_article_ids
                    ).values_list('department_id', flat=True)
                    dept_ids_with_cited.update(cited_dept_ids)
            qs = qs.filter(department__id__in=dept_ids_with_cited)
        else:
            accessible_dept_ids = self._get_user_accessible_department_ids(user)
            if accessible_dept_ids is not None and accessible_dept_ids:
                qs = qs.filter(department__id__in=accessible_dept_ids)
            elif accessible_dept_ids is not None:
                return None

        return qs

    def _get_user_accessible_department_ids(self, user):
        """获取用户可访问的科室 ID 列表
        
        复用 queries.py 中的权限逻辑，确保文章和切片的权限规则一致。
        返回 None 表示不限制（超级管理员），空列表表示无权限。
        """
        from apps.wiki import queries as article_queries
        from apps.base.models import Department

        if not user or not user.is_authenticated:
            public_dept_ids = list(
                Department.objects.filter(is_public=True).values_list("id", flat=True)
            )
            return public_dept_ids

        try:
            patient = user.patient_profile
            patient_dept_ids = list(patient.departments.values_list('id', flat=True))
            if patient_dept_ids:
                from apps.wiki.reference.models import ArticleReferenceStatus
                from django.db.models import Q
                dept_ids = set(
                    Department.objects.filter(
                        Q(is_public=True)
                        | Q(id__in=patient_dept_ids)
                        | Q(
                            article__references_to__target_department_id__in=patient_dept_ids,
                            article__references_to__status=ArticleReferenceStatus.APPROVED,
                        )
                    ).values_list('id', flat=True).distinct()
                )
                return list(dept_ids)
            return list(Department.objects.filter(is_public=True).values_list("id", flat=True))
        except Exception:
            from apps.base.models import UserDepartment
            if (
                hasattr(user, 'role')
                and user.role == UserProfile.Role.SUPER_ADMIN
            ):
                return None
            user_dept_ids = list(
                UserDepartment.objects.filter(user=user)
                .values_list('department_id', flat=True)
                .distinct()
            )
            return user_dept_ids if user_dept_ids else list(
                Department.objects.filter(is_public=True).values_list("id", flat=True)
            )

    def generate_embedding(self, text: str) -> List[float]:
        return self._generate_embedding(text)