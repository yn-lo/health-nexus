from typing import Optional, List
from django.contrib.auth import get_user_model
from apps.chat.models import Message, Conversation

User = get_user_model()


class FeedbackResult:
    def __init__(self, success: bool, error: str = None, message: Message = None):
        self.success = success
        self.error = error
        self.message = message


class FeedbackService:
    """Service for handling message feedback with proper authorization"""

    def submit_feedback(
        self,
        message_id: int,
        user,
        feedback_value: int,
        reason: str = ""
    ) -> FeedbackResult:
        """Submit feedback for a message if user has permission"""
        try:
            message = Message.objects.select_related(
                'conversation__patient__user'
            ).get(id=message_id)
        except Message.DoesNotExist:
            return FeedbackResult(False, "消息不存在")

        if not self._can_user_access_message(user, message):
            return FeedbackResult(False, "无权限操作此消息")

        if feedback_value not in [1, -1, 0]:
            return FeedbackResult(False, "无效的反馈值")

        if message.feedback == feedback_value:
            message.feedback = 0
            message.feedback_reason = ""
        else:
            message.feedback = feedback_value
            if feedback_value == Message.Feedback.DISLIKE:
                valid_reasons = [r.value for r in Message.FeedbackReason]
                if not reason or reason not in valid_reasons:
                    return FeedbackResult(False, "请选择差评原因")
                message.feedback_reason = reason
                message.review_status = Message.ReviewStatus.PENDING
            else:
                message.feedback_reason = ""

        message.save(update_fields=['feedback', 'feedback_reason', 'review_status'])
        return FeedbackResult(True, message=message)

    def _can_user_access_message(self, user, message: Message) -> bool:
        """Check if user owns the conversation this message belongs to"""
        if not user.is_authenticated:
            return False
        conversation = message.conversation
        return conversation.patient.user_id == user.id

    def get_disliked_messages_with_details(self, department_id: int = None, limit: int = 100) -> List[Message]:
        """获取差评消息及其详情，用于待优化队列"""
        qs = Message.objects.filter(
            feedback=Message.Feedback.DISLIKE
        ).select_related(
            'conversation__patient__user',
            'conversation__patient',
            'reviewed_by'
        ).prefetch_related(
            'conversation__patient__departments'
        ).order_by('-created_at')

        if department_id:
            qs = qs.filter(conversation__patient__departments__id=department_id)

        return list(qs[:limit])
