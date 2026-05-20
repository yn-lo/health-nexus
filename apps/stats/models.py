"""
stats 领域数据模型
"""
from django.db import models
from django.utils.translation import gettext_lazy as _

from apps.base.models import TimeStampedModel


class DailyStats(TimeStampedModel):
    """
    每日统计数据表
    按日/科室聚合各维度的运营数据，用于运营看板展示
    """
    date = models.DateField(_("统计日期"), db_index=True)
    department = models.ForeignKey(
        'base.Department',
        on_delete=models.CASCADE,
        related_name='daily_stats',
        verbose_name=_("科室"),
        default=1,
    )

    total_questions = models.IntegerField(_("总提问数"), default=0)
    answered_questions = models.IntegerField(_("AI成功回答数"), default=0)
    rejected_questions = models.IntegerField(_("拒答数"), default=0)
    intercepted_questions = models.IntegerField(_("拦截数"), default=0)
    crisis_questions = models.IntegerField(_("危机干预数"), default=0)

    active_users = models.IntegerField(_("活跃用户数"), default=0)

    article_views = models.IntegerField(_("文章阅读量"), default=0)

    positive_feedbacks = models.IntegerField(_("好评数"), default=0)
    negative_feedbacks = models.IntegerField(_("差评数"), default=0)

    class Meta:
        verbose_name = _("每日统计")
        verbose_name_plural = _("每日统计")
        ordering = ['-date', '-department']
        unique_together = ('date', 'department')

    def __str__(self):
        return f"{self.department} - {self.date} 统计数据"

    @property
    def knowledge_coverage_rate(self):
        """BR-STATS-05: 知识覆盖率 = 有效回答数 / (有效回答数 + 拒答数)"""
        denominator = self.answered_questions + self.rejected_questions
        if denominator == 0:
            return 0
        return round(self.answered_questions / denominator * 100, 2)

    @property
    def rejection_rate(self):
        """BR-STATS-06: 拒答率 = 拒答数 / (有效回答数 + 拒答数)"""
        denominator = self.answered_questions + self.rejected_questions
        if denominator == 0:
            return 0
        return round(self.rejected_questions / denominator * 100, 2)

    @property
    def satisfaction_rate(self):
        """好评率 = 好评数 / (好评数 + 差评数)"""
        total = self.positive_feedbacks + self.negative_feedbacks
        if total == 0:
            return 0
        return round(self.positive_feedbacks / total * 100, 2)
