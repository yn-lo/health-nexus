import logging
from typing import Optional, List, Tuple

from apps.base.models import Department
from apps.wiki.models import ArticleChunk
from apps.service_container import container
from apps.config.services import SafetyConfigService
from .prompt import build_patient_context, build_context_string, build_system_prompt
from .prompt_config import get_prompt_template
from .services.answer_cache_service import AnswerCacheService

logger = logging.getLogger(__name__)


class RAGService:

    def __init__(self):
        self._knowledge_svc = None

    @property
    def knowledge_svc(self):
        if self._knowledge_svc is None:
            self._knowledge_svc = container.knowledge_search_service
        return self._knowledge_svc

    def rag_retrieve(self, query, patient=None, selected_departments=None, health_data_share_enabled=True):
        if not self.knowledge_svc:
            return "", []

        if not selected_departments:
            return "", []

        dept_ids = [d.id for d in selected_departments] if selected_departments else []
        knowledge_version = AnswerCacheService.get_knowledge_version()
        cache_key = AnswerCacheService.get_cache_key(
            query, patient, dept_ids, knowledge_version
        )
        cached = AnswerCacheService.get_cached_answer(cache_key)
        if cached:
            logger.debug("Answer cache hit for query: %s", query[:50])
            chunk_ids = [c["id"] for c in cached.get("chunks", []) if isinstance(c, dict) and "id" in c]
            if chunk_ids:
                relevant_chunks = list(ArticleChunk.objects.filter(id__in=chunk_ids).select_related('article'))
            else:
                relevant_chunks = []
            context_str = build_context_string(relevant_chunks)
            patient_info = build_patient_context(patient, health_data_share_enabled)
            system_prompt = build_system_prompt(patient_info, context_str)
            return system_prompt, relevant_chunks

        relevant_chunks = self.knowledge_svc.search_similar_chunks(query, selected_departments, top_k=3)

        template = get_prompt_template()

        similarity_threshold = SafetyConfigService.get_similarity_threshold()
        if not relevant_chunks:
            return "", []

        filtered_chunks = [
            c for c in relevant_chunks
            if not hasattr(c, 'distance') or c.distance <= similarity_threshold
        ]
        if not filtered_chunks:
            return "", []

        patient_info = build_patient_context(patient, health_data_share_enabled)
        context_str = build_context_string(filtered_chunks)
        system_prompt = build_system_prompt(patient_info, context_str)

        return system_prompt, filtered_chunks

    def search_similar_chunks(self, query, patient=None, selected_departments=None, top_k=3) -> List[ArticleChunk]:
        if not self.knowledge_svc:
            return []

        if not selected_departments:
            return []

        return self.knowledge_svc.search_similar_chunks(query, selected_departments, top_k)
