import hashlib
import logging
from typing import Optional

from django.conf import settings

logger = logging.getLogger(__name__)

ANSWER_CACHE_PREFIX = "chat:answer:"
ANSWER_CACHE_TTL = getattr(settings, 'CHAT_ANSWER_CACHE_TTL', 3600)


class AnswerCacheService:
    """RAG 答案缓存服务
    
    确保相同问题在相同条件下得到相同答案（可复现性）。
    缓存键由查询内容、患者档案快照、科室ID、知识库版本等生成。
    
    注意：
    - 患者档案更新时，旧缓存自动失效（通过 updated_at 哈希）
    - 知识库更新时，需要手动调用 invalidate_by_knowledge_version()
    """

    @staticmethod
    def get_patient_profile_hash(patient) -> Optional[str]:
        """生成患者档案的哈希值，包含所有影响答案的字段
        
        当患者档案更新时，哈希值变化，旧缓存自动失效。
        """
        if not patient:
            return None
        
        parts = [
            str(patient.id),
            str(getattr(patient, 'age', '')),
            str(getattr(patient, 'gender', '')),
            str(getattr(patient, 'medical_history_summary', '')),
            str(getattr(patient, 'allergies_summary', '')),
            str(getattr(patient, 'latest_vitals', '')),
        ]
        
        content = "|".join(parts)
        return hashlib.md5(content.encode('utf-8')).hexdigest()

    @staticmethod
    def get_cache_key(query: str, patient=None, department_ids: Optional[list] = None, 
                      knowledge_version: Optional[str] = None) -> str:
        """生成缓存键
        
        Args:
            query: 用户查询
            patient: 患者档案对象（用于获取档案快照哈希）
            department_ids: 科室ID列表（用于限定知识库范围）
            knowledge_version: 知识库版本号（知识库更新时递增）
            
        Returns:
            缓存键字符串
        """
        dept_str = ",".join(sorted(map(str, department_ids))) if department_ids else ""
        patient_hash = AnswerCacheService.get_patient_profile_hash(patient)
        
        content = f"{query}:{patient_hash or ''}:{dept_str}:{knowledge_version or ''}"
        hash_value = hashlib.md5(content.encode('utf-8')).hexdigest()
        return f"{ANSWER_CACHE_PREFIX}{hash_value}"

    @staticmethod
    def get_cached_answer(cache_key: str):
        """获取缓存答案
        
        Args:
            cache_key: 缓存键
            
        Returns:
            缓存的答案（answer, chunks）或 None
        """
        if not cache_key:
            return None

        try:
            from django.core.cache import cache
            cached = cache.get(cache_key)
            if cached:
                logger.debug(f"Cache hit for key: {cache_key}")
            return cached
        except Exception as e:
            logger.error(f"Failed to get cached answer: {e}")
            return None

    @staticmethod
    def cache_answer(cache_key: str, answer: str, chunks: list, ttl: int = ANSWER_CACHE_TTL):
        """缓存答案
        
        Args:
            cache_key: 缓存键
            answer: AI 回答
            chunks: 引用片段列表（ArticleChunk ORM对象或dict）
            ttl: 缓存过期时间（秒）
        """
        if not cache_key or not answer:
            return

        try:
            from django.core.cache import cache
            serializable_chunks = []
            for chunk in chunks:
                if hasattr(chunk, 'id'):
                    serializable_chunks.append({
                        "id": chunk.id,
                        "content_text": chunk.content_text,
                        "chunk_index": getattr(chunk, 'chunk_index', 0),
                    })
                else:
                    serializable_chunks.append(chunk)
            cache.set(cache_key, {"answer": answer, "chunks": serializable_chunks}, ttl)
            logger.debug(f"Cached answer for key: {cache_key}")
        except Exception as e:
            logger.error(f"Failed to cache answer: {e}")

    @staticmethod
    def invalidate_cache(cache_key: str):
        """使缓存失效
        
        Args:
            cache_key: 缓存键
        """
        if not cache_key:
            return

        try:
            from django.core.cache import cache
            cache.delete(cache_key)
            logger.debug(f"Invalidated cache for key: {cache_key}")
        except Exception as e:
            logger.error(f"Failed to invalidate cache: {e}")

    @staticmethod
    def invalidate_by_knowledge_version():
        """知识库更新后，使所有答案缓存失效
        
        当知识库（文章/切片）更新时调用，确保不再返回基于旧知识的答案。
        由于 Django cache API 不直接支持通配符删除，这里采用版本号策略：
        每次知识库更新后，递增版本号存储到缓存，旧缓存因版本号不匹配自动失效。
        """
        try:
            from django.core.cache import cache
            version_key = "chat:knowledge_version"
            current = cache.get(version_key, 0)
            cache.set(version_key, current + 1, timeout=86400 * 30)  # 30天过期
            logger.info(f"Knowledge version incremented to {current + 1}")
        except Exception as e:
            logger.error(f"Failed to increment knowledge version: {e}")

    @staticmethod
    def get_knowledge_version() -> str:
        """获取当前知识库版本号
        
        Returns:
            版本号字符串
        """
        try:
            from django.core.cache import cache
            version = cache.get("chat:knowledge_version", 0)
            return str(version)
        except Exception as e:
            import logging
            logging.getLogger(__name__).warning("KNOWLEDGE_VERSION_FALLBACK | reason=%s", str(e), exc_info=True)
            return "0"

    @staticmethod
    def invalidate_by_pattern(pattern: str):
        """按模式使缓存失效
        
        注意：Django Cache API 不支持通配符删除。
        请使用 invalidate_by_knowledge_version() 代替，
        它通过版本号策略使所有缓存自动失效。
        
        Args:
            pattern: 缓存键前缀模式（此参数当前被忽略）
        """
        logger.warning(
            "invalidate_by_pattern 不被支持，请使用 invalidate_by_knowledge_version() 代替"
        )
        AnswerCacheService.invalidate_by_knowledge_version()
