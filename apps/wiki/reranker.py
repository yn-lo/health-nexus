import logging
from dataclasses import dataclass
from typing import List

import requests

logger = logging.getLogger(__name__)


@dataclass
class RerankResult:
    index: int
    relevance_score: float


class RerankerService:

    def __init__(self, base_url: str = None, api_key: str = None, model_name: str = None, top_n: int = None):
        self._base_url = base_url
        self._api_key = api_key
        self._model_name = model_name
        self._top_n = top_n
        self._session = requests.Session()

    def _resolve_config(self, key: str, default=None):
        if getattr(self, f"_{key}", None):
            return getattr(self, f"_{key}")
        config = self._get_config()
        return config.get(key, default)

    def _get_config(self) -> dict:
        from apps.config.services.ai_service import AIConfigService
        return AIConfigService.get_next_config('RERANK')

    @staticmethod
    def _get_rerank_threshold() -> float:
        from apps.config.models import RAGConfig
        try:
            config = RAGConfig.objects.get(config_key='rerank_threshold')
            return float(config.config_value)
        except (RAGConfig.DoesNotExist, ValueError, TypeError):
            return 0.7

    def rerank(self, query: str, chunks: list, top_k: int = 5) -> list:
        if not chunks:
            return []

        if len(chunks) <= top_k and len(chunks) <= 3:
            return chunks

        try:
            scores = self._call_rerank_api(query, chunks)
            threshold = self._get_rerank_threshold()

            filtered_scores = [s for s in scores if s.relevance_score >= threshold]

            if not filtered_scores:
                logger.warning(
                    "RERANK_ALL_BELOW_THRESHOLD | query=%s | threshold=%.2f | reason=all_scores_below_threshold",
                    query[:50], threshold,
                )
                return chunks[:top_k]

            sorted_scores = sorted(filtered_scores, key=lambda s: s.relevance_score, reverse=True)

            result = [chunks[s.index] for s in sorted_scores[:top_k] if s.index < len(chunks)]

            filtered_count = len(scores) - len(filtered_scores)
            if filtered_count > 0:
                logger.info(
                    "RERANK_FILTERED | query=%s | total=%d | filtered=%d | threshold=%.2f",
                    query[:50], len(scores), filtered_count, threshold,
                )

            return result

        except Exception:
            logger.warning(
                "RERANK_FALLBACK | query=%s | chunks=%d | reason=api_failure",
                query[:50], len(chunks),
                exc_info=True,
            )
            return chunks[:top_k]

    def _call_rerank_api(self, query: str, chunks: list) -> List[RerankResult]:
        base = self._resolve_config('base_url', '').rstrip('/')
        url = f"{base}/rerank"

        headers = {
            "Authorization": f"Bearer {self._resolve_config('api_key', '')}",
            "Content-Type": "application/json",
        }

        documents = [chunk.content_text for chunk in chunks]
        top_n = self._resolve_config('top_n', len(documents))

        payload = {
            "model": self._resolve_config('model_name', ''),
            "query": query,
            "documents": documents,
            "top_n": top_n if top_n else len(documents),
        }

        response = self._session.post(
            url, json=payload, headers=headers,
            timeout=self._resolve_config('timeout', 10),
        )
        response.raise_for_status()
        data = response.json()

        results = []
        for item in data.get("results", []):
            results.append(RerankResult(
                index=item.get("index", 0),
                relevance_score=item.get("relevance_score", 0.0),
            ))

        return results
