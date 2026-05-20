"""
StatsService - 运营统计数据服务

职责：
- 知识覆盖率统计 (BR-STATS-05/06)
- 差评分布统计 (BR-STATS-07)
- 用户活跃度 (DAU/WAU/MAU)
- 文章阅读量统计
- 反馈统计
- 每日数据聚合 (AC-STATS-06)
"""
import logging
from datetime import date, timedelta
from typing import Optional

from django.db.models import Count, Sum, Q
from django.utils import timezone

from apps.stats.models import DailyStats

logger = logging.getLogger(__name__)


class StatsService:

    @staticmethod
    def get_knowledge_coverage(
        start_date: Optional[date] = None,
        end_date: Optional[date] = None,
        department_id=None
    ) -> dict:
        """AC-STATS-02: 获取知识覆盖率统计"""
        if start_date and end_date and end_date < start_date:
            raise ValueError("结束日期不能早于开始日期")

        qs = DailyStats.objects.all()
        if start_date:
            qs = qs.filter(date__gte=start_date)
        if end_date:
            qs = qs.filter(date__lte=end_date)
        if department_id:
            qs = qs.filter(department_id=department_id)

        total = qs.aggregate(
            total_questions=Sum('total_questions'),
            answered_questions=Sum('answered_questions'),
            rejected_questions=Sum('rejected_questions'),
            intercepted_questions=Sum('intercepted_questions'),
            crisis_questions=Sum('crisis_questions'),
        )

        total_questions = total['total_questions'] or 0
        answered_questions = total['answered_questions'] or 0
        rejected_questions = total['rejected_questions'] or 0
        intercepted_questions = total['intercepted_questions'] or 0
        crisis_questions = total['crisis_questions'] or 0

        coverage_denominator = answered_questions + rejected_questions
        coverage_rate = round(answered_questions / coverage_denominator * 100, 2) if coverage_denominator > 0 else 0
        rejection_rate = round(rejected_questions / coverage_denominator * 100, 2) if coverage_denominator > 0 else 0

        return {
            'total_questions': total_questions,
            'answered_questions': answered_questions,
            'rejected_questions': rejected_questions,
            'intercepted_questions': intercepted_questions,
            'crisis_questions': crisis_questions,
            'coverage_rate': coverage_rate,
            'rejection_rate': rejection_rate,
        }

    @staticmethod
    def get_feedback_stats(
        start_date: Optional[date] = None,
        end_date: Optional[date] = None,
        department_id=None
    ) -> dict:
        """AC-STATS-03: 获取反馈统计（含差评原因分布）"""
        qs = DailyStats.objects.all()
        if start_date:
            qs = qs.filter(date__gte=start_date)
        if end_date:
            qs = qs.filter(date__lte=end_date)
        if department_id:
            qs = qs.filter(department_id=department_id)

        total = qs.aggregate(
            positive_feedbacks=Sum('positive_feedbacks'),
            negative_feedbacks=Sum('negative_feedbacks'),
        )

        positive = total['positive_feedbacks'] or 0
        negative = total['negative_feedbacks'] or 0
        total_feedbacks = positive + negative

        satisfaction_rate = round(positive / total_feedbacks * 100, 2) if total_feedbacks > 0 else 0

        dislike_reasons = StatsService._get_dislike_reasons(
            start_date, end_date, department_id
        )

        return {
            'positive_feedbacks': positive,
            'negative_feedbacks': negative,
            'satisfaction_rate': satisfaction_rate,
            'dislike_reasons': dislike_reasons,
        }

    @staticmethod
    def _get_dislike_reasons(
        start_date: Optional[date] = None,
        end_date: Optional[date] = None,
        department_id=None
    ) -> list:
        """BR-STATS-07: 从 Message.feedback_reason 聚合差评原因分布"""
        from apps.chat.models import Message
        from datetime import datetime

        qs = Message.objects.filter(
            feedback=Message.Feedback.DISLIKE,
            feedback_reason__in=[r[0] for r in Message.FeedbackReason.choices],
        )
        if start_date:
            start_dt = timezone.make_aware(datetime.combine(start_date, datetime.min.time()))
            qs = qs.filter(created_at__gte=start_dt)
        if end_date:
            end_dt = timezone.make_aware(datetime.combine(end_date + timedelta(days=1), datetime.min.time()))
            qs = qs.filter(created_at__lt=end_dt)
        if department_id:
            qs = qs.filter(
                conversation__departments__id=department_id
            )

        reasons = qs.values('feedback_reason').annotate(
            count=Count('id')
        ).order_by('-count')

        return [{'reason': r['feedback_reason'], 'count': r['count']} for r in reasons]

    @staticmethod
    def get_user_activity(days: int = 30) -> dict:
        """AC-STATS-01: 获取用户活跃度统计"""
        today = timezone.now().date()

        qs = DailyStats.objects.filter(
            date__gte=today - timedelta(days=days - 1),
            date__lte=today,
        )

        today_stats = DailyStats.objects.filter(date=today).first()
        dau = today_stats.active_users if today_stats else 0

        wau_qs = qs.filter(date__gte=today - timedelta(days=6))
        wau_total = wau_qs.aggregate(total=Sum('active_users'))['total'] or 0
        wau = round(wau_total / 7) if wau_total > 0 else 0

        mau_total = qs.aggregate(total=Sum('active_users'))['total'] or 0
        mau = round(mau_total / days) if mau_total > 0 else 0

        return {
            'dau': dau,
            'wau': wau,
            'mau': mau,
            'active_users_trend': list(qs.values('date', 'active_users').order_by('date')),
        }

    @staticmethod
    def get_article_view_stats(
        start_date: Optional[date] = None,
        end_date: Optional[date] = None,
        department_id=None,
        limit: int = 10
    ) -> dict:
        """AC-STATS-01: 获取文章阅读量统计"""
        qs = DailyStats.objects.all()
        if start_date:
            qs = qs.filter(date__gte=start_date)
        if end_date:
            qs = qs.filter(date__lte=end_date)
        if department_id:
            qs = qs.filter(department_id=department_id)

        total_views = qs.aggregate(total=Sum('article_views'))['total'] or 0

        return {
            'total_views': total_views,
        }

    @staticmethod
    def aggregate_daily_stats(target_date: Optional[date] = None) -> dict:
        """AC-STATS-06: 聚合每日统计数据，从 chat 领域 Message 读取"""
        from apps.chat.models import Message
        from apps.base.models import Department
        from datetime import datetime

        if target_date is None:
            target_date = timezone.now().date() - timedelta(days=1)

        logger.info("开始聚合 %s 的统计数据", target_date)

        start_dt = timezone.make_aware(datetime.combine(target_date, datetime.min.time()))
        end_dt = timezone.make_aware(datetime.combine(target_date + timedelta(days=1), datetime.min.time()))

        for dept in Department.objects.all():
            dept_messages = Message.objects.filter(
                conversation__departments=dept,
                sender=Message.Sender.AI,
                created_at__gte=start_dt,
                created_at__lt=end_dt,
            )

            answered = dept_messages.filter(
                processing_result=Message.ProcessingResult.ANSWERED
            ).count()
            rejected = dept_messages.filter(
                processing_result=Message.ProcessingResult.REJECTED
            ).count()
            intercepted = dept_messages.filter(
                processing_result=Message.ProcessingResult.INTERCEPTED
            ).count()
            crisis = dept_messages.filter(
                processing_result=Message.ProcessingResult.CRISIS
            ).count()

            total = answered + rejected + intercepted + crisis

            positive_feedbacks = dept_messages.filter(
                feedback=Message.Feedback.LIKE
            ).count()
            negative_feedbacks = dept_messages.filter(
                feedback=Message.Feedback.DISLIKE
            ).count()

            active_users = Message.objects.filter(
                conversation__departments=dept,
                sender=Message.Sender.USER,
                created_at__gte=start_dt,
                created_at__lt=end_dt,
            ).values('conversation__patient').distinct().count()

            DailyStats.objects.update_or_create(
                date=target_date,
                department=dept,
                defaults={
                    'total_questions': total,
                    'answered_questions': answered,
                    'rejected_questions': rejected,
                    'intercepted_questions': intercepted,
                    'crisis_questions': crisis,
                    'active_users': active_users,
                    'positive_feedbacks': positive_feedbacks,
                    'negative_feedbacks': negative_feedbacks,
                },
            )

        logger.info("完成聚合 %s 的统计数据", target_date)

        return {'date': target_date, 'status': 'completed'}

    @staticmethod
    def get_daily_stats(days: int = 30, department_id=None) -> list:
        """获取每日统计列表"""
        today = timezone.now().date()
        qs = DailyStats.objects.filter(
            date__gte=today - timedelta(days=days - 1),
            date__lte=today,
        )
        if department_id:
            qs = qs.filter(department_id=department_id)
        return list(qs.values(
            'date', 'department__name',
            'total_questions', 'answered_questions', 'rejected_questions',
            'active_users', 'positive_feedbacks', 'negative_feedbacks',
        ).order_by('-date'))

    @staticmethod
    def get_department_stats() -> list:
        """获取各科室汇总统计（超级管理员用）"""
        today = timezone.now().date()
        start = today - timedelta(days=6)
        qs = DailyStats.objects.filter(date__gte=start, date__lte=today)
        dept_stats = qs.values('department__name', 'department_id').annotate(
            total_questions=Sum('total_questions'),
            answered_questions=Sum('answered_questions'),
            rejected_questions=Sum('rejected_questions'),
            positive_feedbacks=Sum('positive_feedbacks'),
            negative_feedbacks=Sum('negative_feedbacks'),
        ).order_by('department__name')

        result = []
        for ds in dept_stats:
            coverage_denominator = (ds['answered_questions'] or 0) + (ds['rejected_questions'] or 0)
            coverage_rate = round(
                (ds['answered_questions'] or 0) / coverage_denominator * 100, 2
            ) if coverage_denominator > 0 else 0

            total_feedbacks = (ds['positive_feedbacks'] or 0) + (ds['negative_feedbacks'] or 0)
            satisfaction_rate = round(
                (ds['positive_feedbacks'] or 0) / total_feedbacks * 100, 2
            ) if total_feedbacks > 0 else 0

            result.append({
                'department': ds['department__name'],
                'department_id': ds['department_id'],
                'total_questions': ds['total_questions'] or 0,
                'coverage_rate': coverage_rate,
                'satisfaction_rate': satisfaction_rate,
                'negative_feedbacks': ds['negative_feedbacks'] or 0,
            })
        return result
