import logging
import re
from typing import Optional, Tuple

from ..models import Conversation, Message

logger = logging.getLogger(__name__)

CONTEXT_TOKEN_WARNING_THRESHOLD = 100_000


class ConversationService:

    @staticmethod
    def generate_title(message: str, max_length: int = 20) -> str:
        if not message or not message.strip():
            return '新对话'

        message = message.strip()
        if len(message) <= max_length:
            return message

        truncated = message[:max_length]
        last_boundary = -1
        for i in range(len(truncated) - 1, -1, -1):
            char = truncated[i]
            if char in (' ', '，', '。', '？', '！', '；', '、'):
                last_boundary = i
                break

        if last_boundary > 0:
            return truncated[:last_boundary]
        return truncated

    @staticmethod
    def create_conversation(patient_profile, title: str = None) -> Conversation:
        return Conversation.objects.create(
            patient=patient_profile,
            title=title or "新对话"
        )

    @staticmethod
    def save_message(conversation, sender, content, reference_chunks=None, feedback=0, is_streaming=False) -> Message:
        message = Message.objects.create(
            conversation=conversation,
            sender=sender,
            content=content,
            feedback=feedback,
            is_streaming=is_streaming,
        )
        if reference_chunks:
            message.reference_chunks.set(reference_chunks)
        return message

    @staticmethod
    def finalize_ai_message(message: Message, content: str, reference_chunks=None):
        """将流式生成中的 AI 消息标记为完成"""
        message.content = content
        message.is_streaming = False
        message.save(update_fields=['content', 'is_streaming'])
        if reference_chunks:
            message.reference_chunks.set(reference_chunks)

    @staticmethod
    def get_disliked_messages(department_id: int = None, limit: int = 50):
        qs = Message.objects.filter(
            feedback=Message.Feedback.DISLIKE
        ).select_related('conversation__patient').prefetch_related('reference_chunks')

        if department_id:
            qs = qs.filter(conversation__patient__departments__id=department_id)

        return qs.order_by('-created_at')[:limit]

    @staticmethod
    def get_conversation_history(conversation_id, patient=None):
        qs = Conversation.objects.filter(id=conversation_id)
        if patient:
            qs = qs.filter(patient=patient)
        conversation = qs.select_related('patient').first()
        if not conversation:
            return None, []
        messages = conversation.messages.all().prefetch_related('reference_chunks').order_by('created_at')
        return conversation, messages

    @staticmethod
    def get_recent_messages(conversation_id, patient=None, limit=None):
        """获取会话历史消息对

        Args:
            conversation_id: 会话ID
            patient: 患者对象（用于权限校验）
            limit: 最大返回轮数，None表示不限制（使用全部历史）

        Returns:
            [(user_msg, ai_msg), ...] 列表
        """
        qs = Conversation.objects.filter(id=conversation_id)
        if patient:
            qs = qs.filter(patient=patient)
        conversation = qs.first()
        if not conversation:
            return []

        messages = list(
            conversation.messages.all()
            .prefetch_related('reference_chunks')
            .order_by('created_at')
        )

        pairs = []
        last_user_msg = None

        for msg in messages:
            if msg.sender == Message.Sender.USER:
                last_user_msg = msg
            elif msg.sender == Message.Sender.AI and last_user_msg and not msg.is_streaming:
                pairs.append((last_user_msg, msg))
                last_user_msg = None

        if limit and len(pairs) > limit:
            return pairs[-limit:]
        return pairs

    @staticmethod
    def estimate_tokens(text: str) -> int:
        """估算文本的Token数量

        使用启发式估算：
        - 中文字符：每个约1.5 tokens
        - 英文单词：每个约1.3 tokens
        - 其他字符：每4个约1 token
        """
        if not text:
            return 0

        chinese_chars = len(re.findall(r'[\u4e00-\u9fff]', text))
        english_words = len(re.findall(r'[a-zA-Z]+', text))
        other_chars = len(text) - chinese_chars - len(''.join(re.findall(r'[a-zA-Z]+', text)))

        tokens = int(chinese_chars * 1.5 + english_words * 1.3 + other_chars / 4)
        return tokens

    @staticmethod
    def estimate_conversation_tokens(conversation_id, patient=None) -> Tuple[int, str]:
        """估算会话的总Token数

        Returns:
            (total_tokens, context_summary)
            context_summary 包含患者档案摘要 + 所有历史消息
        """
        qs = Conversation.objects.filter(id=conversation_id)
        if patient:
            qs = qs.filter(patient=patient)
        conversation = qs.select_related('patient').first()
        if not conversation:
            return 0, ""

        total_tokens = 0

        # 估算患者档案
        if conversation.patient:
            patient_info = []
            if conversation.patient.name:
                patient_info.append(conversation.patient.name)
            if conversation.patient.age:
                patient_info.append(f"{conversation.patient.age}岁")
            if conversation.patient.medical_history_summary:
                patient_info.append(conversation.patient.medical_history_summary)
            patient_info_str = '，'.join(patient_info)
            total_tokens += ConversationService.estimate_tokens(patient_info_str)

        # 估算历史消息
        messages = conversation.messages.all().order_by('created_at')
        for msg in messages:
            total_tokens += ConversationService.estimate_tokens(msg.content)

        return total_tokens, f"{conversation.title or '健康咨询'}"

    @staticmethod
    def check_context_warning(conversation_id, patient=None) -> Tuple[bool, int]:
        """检查是否需要触发上下文预警

        Returns:
            (should_warn, estimated_tokens)
        """
        total_tokens, _ = ConversationService.estimate_conversation_tokens(conversation_id, patient)
        return total_tokens >= CONTEXT_TOKEN_WARNING_THRESHOLD, total_tokens

    @staticmethod
    def get_health_data_share_enabled(patient) -> bool:
        """获取患者健康数据共享开关状态"""
        if not patient:
            return False
        return getattr(patient.user, 'health_data_share_enabled', True)

    @staticmethod
    def migrate_anonymous_conversations(session_key, patient) -> int:
        if not session_key or not patient:
            return 0

        anonymous_conversations = Conversation.objects.filter(
            session_key=session_key,
            patient__isnull=True
        )

        migrated = 0
        for conv in anonymous_conversations:
            existing = Conversation.objects.filter(
                patient=patient,
                title=conv.title
            ).exclude(id=conv.id).first()
            if existing:
                suffix = 1
                while Conversation.objects.filter(
                    patient=patient,
                    title=f"{conv.title} - {suffix}"
                ).exists():
                    suffix += 1
                conv.title = f"{conv.title} - {suffix}"

            conv.patient = patient
            conv.session_key = ''
            conv.save(update_fields=['patient', 'session_key', 'title'])
            migrated += 1

        logger.info("Migrated %d anonymous conversations for patient %s", migrated, patient.id)
        return migrated
