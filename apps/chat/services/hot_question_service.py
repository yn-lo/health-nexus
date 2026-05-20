from django.core.cache import cache
from django.db.models import Q
from apps.chat.models import HotQuestion

CACHE_TTL = 7200

DEFAULT_QUESTIONS = [
    '什么是高血压？有哪些常见症状？',
    '如何预防糖尿病？日常饮食要注意什么？',
    '感冒和流感有什么区别？',
]


class HotQuestionService:
    @staticmethod
    def get_hot_questions(department_ids=None, department_id=None):
        if department_id is not None:
            department_ids = [department_id]
        if department_ids is not None and not isinstance(department_ids, (list, tuple, set)):
            department_ids = [department_ids]
        if department_ids:
            cache_key = f'hot_questions:ids:{",".join(map(str, sorted(department_ids)))}'
        else:
            cache_key = 'hot_questions:global'
        cached = cache.get(cache_key)
        if cached is not None:
            return cached

        qs = HotQuestion.objects.filter(is_active=True)
        if department_ids:
            qs = qs.filter(Q(department_id__in=department_ids) | Q(department__isnull=True))

        questions = [q.question for q in qs.order_by('sort_order', 'id')]

        if not questions:
            questions = list(DEFAULT_QUESTIONS)

        cache.set(cache_key, questions, CACHE_TTL)
        return questions

    @staticmethod
    def invalidate_cache(department_id=None):
        cache_key = f'hot_questions:{department_id or "global"}'
        cache.delete(cache_key)
