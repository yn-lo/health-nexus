"""
Hybrid Search - 混合检索模块（BM25 + 向量检索 + RRF 融合）

结合向量语义检索和 PostgreSQL 全文检索（BM25）：
- 向量检索：理解语义（"心脏病" ≈ "冠心病"）
- BM25 检索：精确匹配关键词（药品名、检查指标）
- RRF (Reciprocal Rank Fusion)：融合两种检索结果

参考 2025 RAG 最佳实践：Hybrid 检索 top-1 召回率从 48.7% 提升到 53.4%。
"""
import logging
from dataclasses import dataclass, field
from typing import List, Optional

logger = logging.getLogger(__name__)


@dataclass
class HybridSearchResult:
    chunk: object
    rrf_score: float
    vector_rank: Optional[int] = None
    bm25_rank: Optional[int] = None


def reciprocal_rank_fusion(
    vector_results: list,
    bm25_results: list,
    k: int = 60,
    top_k: Optional[int] = None,
) -> List[HybridSearchResult]:
    """
    Reciprocal Rank Fusion (RRF) 算法。

    对两组检索结果进行融合排序：
    score(d) = 1/(k + rank_vector(d)) + 1/(k + rank_bm25(d))

    Args:
        vector_results: 向量检索结果列表（按相似度降序）
        bm25_results: BM25 检索结果列表（按相关性降序）
        k: RRF 常数，默认 60（原论文推荐值）
        top_k: 返回前 top_k 个结果，None 表示返回全部

    Returns:
        按 RRF 分数降序排列的 HybridSearchResult 列表
    """
    if not vector_results and not bm25_results:
        return []

    scores = {}

    for rank, chunk in enumerate(vector_results, start=1):
        pk = chunk.pk
        if pk not in scores:
            scores[pk] = {"chunk": chunk, "vector_rank": rank, "bm25_rank": None, "score": 0.0}
        scores[pk]["vector_rank"] = rank
        scores[pk]["score"] += 1.0 / (k + rank)

    for rank, chunk in enumerate(bm25_results, start=1):
        pk = chunk.pk
        if pk not in scores:
            scores[pk] = {"chunk": chunk, "vector_rank": None, "bm25_rank": rank, "score": 0.0}
        scores[pk]["bm25_rank"] = rank
        scores[pk]["score"] += 1.0 / (k + rank)

    sorted_results = sorted(scores.values(), key=lambda x: x["score"], reverse=True)

    if top_k is not None:
        sorted_results = sorted_results[:top_k]

    return [
        HybridSearchResult(
            chunk=item["chunk"],
            rrf_score=item["score"],
            vector_rank=item["vector_rank"],
            bm25_rank=item["bm25_rank"],
        )
        for item in sorted_results
    ]
