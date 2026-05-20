import uuid
from django.db import models
from django.utils.html import strip_tags
from django.utils.translation import gettext_lazy as _
from apps.base.models import Department, TimeStampedModel
from apps.care.models import PatientProfile
from apps.wiki.models import ArticleChunk
from apps.auth.models import UserProfile


class PromptTemplateType(models.TextChoices):
    SYSTEM = 'SYSTEM', _('系统提示词')
    REJECTION = 'REJECTION', _('拒答消息')
    EMERGENCY = 'EMERGENCY', _('紧急情况响应')
    SAFETY_WARNING = 'SAFETY_WARNING', _('安全警告')


class PromptTemplate(TimeStampedModel):
    """
    Prompt 模板存储模型

    支持：
    - 多类型模板（SYSTEM, REJECTION, EMERGENCY, SAFETY_WARNING）
    - 多版本管理（通过 is_active 控制当前生效版本）
    - 数据库存储，满足项目宪法 V.1 要求
    """
    type = models.CharField(
        _("模板类型"),
        max_length=20,
        choices=PromptTemplateType.choices,
    )
    content = models.TextField(_("模板内容"))
    is_active = models.BooleanField(_("是否启用"), default=True)

    class Meta:
        verbose_name = _("Prompt模板")
        verbose_name_plural = _("Prompt模板")
        constraints = [
            models.UniqueConstraint(
                fields=['type'],
                condition=models.Q(is_active=True),
                name='unique_active_prompt_per_type'
            )
        ]

    def __str__(self):
        return f"{self.get_type_display()} ({'启用' if self.is_active else '禁用'})"

    @classmethod
    def get_active_template(cls, template_type: PromptTemplateType) -> 'PromptTemplate | None':
        """获取指定类型的启用模板"""
        return cls.objects.filter(type=template_type, is_active=True).first()

    @classmethod
    def get_all_active_templates(cls) -> dict:
        """获取所有启用模板的字典，键为类型"""
        templates = cls.objects.filter(is_active=True)
        return {t.type: t.content for t in templates}


class Conversation(TimeStampedModel):
    """会话容器"""
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    patient = models.ForeignKey(
        PatientProfile, on_delete=models.CASCADE, related_name='conversations',
        null=True, blank=True
    )
    session_key = models.CharField(max_length=40, blank=True, db_index=True)
    title = models.CharField(_("会话标题"), max_length=100, blank=True)
    is_archived = models.BooleanField(_("已归档"), default=False, db_index=True)
    archived_at = models.DateTimeField(_("归档时间"), null=True, blank=True)
    departments = models.ManyToManyField(
        Department, related_name='conversations', blank=True,
        verbose_name=_("锁定科室")
    )

    class Meta:
        ordering = ['-updated_at']
        verbose_name = _("AI会话")

    def __str__(self):
        patient_name = self.patient.name if self.patient else 'Unknown'
        return f"{patient_name} - {self.created_at.strftime('%m-%d %H:%M')}"

    @property
    def last_message(self):
        """获取会话的最后一条消息内容（排除流式生成中的消息）"""
        msg = self.messages.filter(is_streaming=False).order_by('-created_at').first()
        if msg:
            stripped = strip_tags(msg.content)
            text = stripped[:50]
            return text + '...' if len(stripped) > 50 else text
        return ''


class Message(TimeStampedModel):
    """消息明细"""
    class Sender(models.TextChoices):
        USER = 'USER', _('用户')
        AI = 'AI', _('AI助手')

    class Feedback(models.IntegerChoices):
        NONE = 0, _('未评价')
        LIKE = 1, _('有用')
        DISLIKE = -1, _('无用/错误')

    class ReviewStatus(models.IntegerChoices):
        PENDING = 0, _('待处理')
        REVIEWED = 1, _('已处理')
        CORRECTION_NEEDED = 2, _('需修正')

    class CorrectionStatus(models.TextChoices):
        NONE = 'NONE', _('无需修正')
        PENDING = 'PENDING', _('待修正')
        IN_PROGRESS = 'IN_PROGRESS', _('修正中')
        DONE = 'DONE', _('已修正')
        VERIFIED = 'VERIFIED', _('已验证')

    class FeedbackReason(models.TextChoices):
        INACCURATE = 'INACCURATE', _('回答不准确')
        INCOMPLETE = 'INCOMPLETE', _('回答不完整')
        IRRELEVANT = 'IRRELEVANT', _('回答不相关')
        UNSAFE = 'UNSAFE', _('内容不安全')
        OTHER = 'OTHER', _('其他')

    class ProcessingResult(models.TextChoices):
        ANSWERED = 'ANSWERED', _('已回答')
        REJECTED = 'REJECTED', _('拒答')
        INTERCEPTED = 'INTERCEPTED', _('拦截')
        CRISIS = 'CRISIS', _('危机')
        RATE_LIMITED = 'RATE_LIMITED', _('限流')

    conversation = models.ForeignKey(Conversation, on_delete=models.CASCADE, related_name='messages')
    sender = models.CharField(max_length=10, choices=Sender.choices)
    content = models.TextField(_("内容"))
    processing_result = models.CharField(
        _("处理结果"), max_length=20,
        choices=ProcessingResult.choices,
        default=ProcessingResult.ANSWERED,
        db_index=True,
    )

    # 引用源：记录这条回答引用了哪些 Chunk (比引用 Article 更精确)
    reference_chunks = models.ManyToManyField(ArticleChunk, blank=True, verbose_name=_("引用切片"))

    # 流式生成状态
    is_streaming = models.BooleanField(_("是否生成中"), default=False, db_index=True)

    # 安全审计字段
    is_safety_flagged = models.BooleanField(_("是否触发安全检测"), default=False, db_index=True)
    safety_flag_reason = models.CharField(_("安全标记原因"), max_length=200, blank=True, default='')
    safety_flag_level = models.CharField(_("安全标记级别"), max_length=20, blank=True, default='')
    original_content = models.TextField(_("截断前原始内容"), blank=True, default='')

    # 反馈闭环 (PRD V1.1 重点)
    feedback = models.IntegerField(_("用户评价"), choices=Feedback.choices, default=Feedback.NONE)
    feedback_reason = models.CharField(_("差评原因"), max_length=20, choices=FeedbackReason.choices, blank=True)
    review_status = models.IntegerField(_("处理状态"), choices=ReviewStatus.choices, default=ReviewStatus.PENDING)
    reviewed_by = models.ForeignKey(UserProfile, on_delete=models.SET_NULL, null=True, blank=True, verbose_name=_("审核人"))
    reviewed_at = models.DateTimeField(_("审核时间"), null=True, blank=True)
    correction_status = models.CharField(
        _("修正状态"), max_length=20,
        choices=CorrectionStatus.choices,
        default=CorrectionStatus.NONE,
    )
    correction_note = models.TextField(_("修正备注"), blank=True, default='')

    class Meta:
        ordering = ['created_at']
        verbose_name = _("对话消息")

    def __str__(self):
        return f"{self.sender}: {self.content[:20]}..."


class HotQuestion(TimeStampedModel):
    """热门问题配置——由管理员在后台维护"""
    question = models.CharField(_("问题"), max_length=200)
    department = models.ForeignKey(
        Department,
        on_delete=models.CASCADE,
        related_name='hot_questions',
        verbose_name=_("所属科室"),
        null=True,
        blank=True,
    )
    sort_order = models.IntegerField(_("排序"), default=0)
    is_active = models.BooleanField(_("是否启用"), default=True)

    class Meta:
        verbose_name = _("热门问题")
        verbose_name_plural = _("热门问题")
        ordering = ['sort_order', 'id']

    def __str__(self):
        return self.question


class CrisisEvent(TimeStampedModel):
    class Level(models.TextChoices):
        CRISIS = 'CRISIS', _('自伤危机')
        EMERGENCY = 'EMERGENCY', _('紧急症状')

    conversation = models.ForeignKey(
        Conversation, on_delete=models.CASCADE, related_name='crisis_events',
        verbose_name=_("关联会话")
    )
    message = models.ForeignKey(
        Message, on_delete=models.SET_NULL, null=True, blank=True,
        related_name='crisis_events', verbose_name=_("关联消息")
    )
    level = models.CharField(_("危机级别"), max_length=20, choices=Level.choices)
    trigger_text = models.TextField(_("触发内容"), blank=True, default='')
    matched_keywords = models.CharField(_("匹配关键词"), max_length=200, blank=True, default='')
    patient = models.ForeignKey(
        PatientProfile, on_delete=models.SET_NULL, null=True, blank=True,
        related_name='crisis_events', verbose_name=_("患者")
    )
    is_resolved = models.BooleanField(_("已处理"), default=False, db_index=True)
    resolved_by = models.ForeignKey(
        UserProfile, on_delete=models.SET_NULL, null=True, blank=True,
        related_name='resolved_crisis_events', verbose_name=_("处理人")
    )
    resolved_at = models.DateTimeField(_("处理时间"), null=True, blank=True)

    class Meta:
        verbose_name = _("危机事件")
        verbose_name_plural = _("危机事件")
        ordering = ['-created_at']

    def __str__(self):
        return f"[{self.get_level_display()}] {self.trigger_text[:30]}"
