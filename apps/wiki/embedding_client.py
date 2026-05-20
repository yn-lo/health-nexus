import logging
import requests
from typing import Protocol, List, Optional

logger = logging.getLogger(__name__)


class EmbeddingClientStrategy(Protocol):
    def embed(self, text: str) -> List[float]: ...

    def embed_batch(self, texts: List[str]) -> List[List[float]]: ...


class OpenAICompatibleEmbeddingClient:

    def __init__(
        self,
        base_url: str = None,
        api_key: str = None,
        model_name: str = None,
        timeout: int = None,
    ):
        self._base_url = base_url
        self._api_key = api_key
        self._model_name = model_name
        self._timeout = timeout
        self._session = requests.Session()

    def _resolve_config(self, key: str, default=None):
        if getattr(self, f"_{key}", None):
            return getattr(self, f"_{key}")
        config = self._get_config()
        return config.get(key, default)

    def _get_config(self) -> dict:
        from apps.config.services.ai_service import AIConfigService
        return AIConfigService.get_next_config('EMBEDDING')

    def _build_url(self) -> str:
        base = self._resolve_config('base_url', '').rstrip('/')
        if '/embeddings' in base:
            return base
        return f"{base}/embeddings"

    def embed(self, text: str) -> Optional[List[float]]:
        url = self._build_url()
        headers = {
            "Authorization": f"Bearer {self._resolve_config('api_key', '')}",
            "Content-Type": "application/json",
        }
        payload = {"model": self._resolve_config('model_name', ''), "input": text}

        try:
            response = self._session.post(
                url, json=payload, headers=headers,
                timeout=self._resolve_config('timeout', 10),
            )
            response.raise_for_status()
            data = response.json()
            if not data.get("data"):
                logger.error("Embedding API returned empty data array")
                return None
            return data["data"][0]["embedding"]
        except requests.exceptions.Timeout:
            logger.error("Embedding API timeout for text length=%d", len(text))
            raise
        except requests.exceptions.ConnectionError:
            logger.error("Embedding API connection failed")
            raise
        except requests.exceptions.HTTPError as e:
            logger.error("Embedding API HTTP error: %s", e)
            raise
        except (KeyError, IndexError) as e:
            logger.error("Embedding API unexpected response format: %s", e)
            return None

    def embed_batch(self, texts: List[str]) -> List[Optional[List[float]]]:
        if not texts:
            return []

        url = self._build_url()
        headers = {
            "Authorization": f"Bearer {self._resolve_config('api_key', '')}",
            "Content-Type": "application/json",
        }
        payload = {"model": self._resolve_config('model_name', ''), "input": texts}

        try:
            response = self._session.post(
                url, json=payload, headers=headers,
                timeout=self._resolve_config('timeout', 30),
            )
            response.raise_for_status()
            data = response.json()
            raw_results = {}
            for item in data.get("data", []):
                idx = item.get("index", len(raw_results))
                raw_results[idx] = item.get("embedding")
            return [raw_results.get(i) for i in range(len(raw_results))]
        except requests.exceptions.Timeout:
            logger.error("Embedding batch API timeout for %d texts", len(texts))
            raise
        except requests.exceptions.ConnectionError:
            logger.error("Embedding batch API connection failed")
            raise
        except requests.exceptions.HTTPError as e:
            logger.error("Embedding batch API HTTP error: %s", e)
            raise


_default_client = None


def _get_client() -> OpenAICompatibleEmbeddingClient:
    global _default_client
    if _default_client is None:
        _default_client = OpenAICompatibleEmbeddingClient()
    return _default_client


def generate_embedding(text: str) -> Optional[List[float]]:
    return _get_client().embed(text)


def generate_embeddings_batch(texts: List[str]) -> List[Optional[List[float]]]:
    return _get_client().embed_batch(texts)
