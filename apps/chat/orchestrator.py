from typing import Generator, List, Optional, Tuple
import logging

from django.shortcuts import get_object_or_404

from apps.base.models import Department
from apps.care.models import PatientProfile
from apps.service_container import container
from .models import Conversation, Message
from .query_rewriter import UnifiedSafetyRewriter
from .services.conversation_service import ConversationService
from .strategies import RuleBasedInputFilter, InputSafetyResult

logger = logging.getLogger(__name__)


class ChatSendOrchestrator:

    def __init__(self, rag_svc=None, chat_mgmt_svc=None, llm_client=None):
        self._rag_svc = rag_svc
        self._chat_mgmt_svc = chat_mgmt_svc
        self._llm_client = llm_client
        self._rule_filter = RuleBasedInputFilter()
        self._rewriter = None

    @property
    def rag_svc(self):
        if self._rag_svc is None:
            self._rag_svc = container.rag_service
        return self._rag_svc

    @property
    def chat_mgmt_svc(self):
        if self._chat_mgmt_svc is None:
            self._chat_mgmt_svc = container.chat_management_service
        return self._chat_mgmt_svc

    @property
    def llm_client(self):
        if self._llm_client is None:
            self._llm_client = self._get_llm_client()
        return self._llm_client

    @property
    def rewriter(self):
        if self._rewriter is None:
            self._rewriter = UnifiedSafetyRewriter(self.llm_client)
        return self._rewriter

    def _get_llm_client(self):
        from apps.chat.llm_client import get_llm_client
        return get_llm_client()

    def resolve_patient(self, user) -> Optional[PatientProfile]:
        if not user.is_authenticated:
            return None
        try:
            return user.patient_profile
        except Exception as e:
            logger.warning("PATIENT_PROFILE_FALLBACK | reason=%s", str(e), exc_info=True)
            return None

    def filter_departments(
        self,
        selected_dept_ids: List[int],
        patient: Optional[PatientProfile],
    ) -> List[Department]:
        if not selected_dept_ids:
            return []

        candidate_depts = list(Department.objects.filter(id__in=selected_dept_ids))

        leaf_dept_ids = set(
            Department.objects.filter(
                id__in=selected_dept_ids,
                parent__isnull=True
            ).values_list('id', flat=True)
        )
        non_leaf_dept_ids = set(selected_dept_ids) - leaf_dept_ids
        if non_leaf_dept_ids:
            candidate_depts = [d for d in candidate_depts if d.id not in non_leaf_dept_ids]

        if patient:
            allowed_dept_ids = set(
                patient.departments.values_list('id', flat=True)
            )
            return [d for d in candidate_depts if d.id in allowed_dept_ids]
        else:
            return candidate_depts

    def classify_input_safety(self, message: str, history: list = None):
        """两层输入安全审查：规则层 + LLM层

        流程：
        1. 规则层快速拦截（危机/注入）→ 命中则直接返回，跳过LLM
        2. 规则层返回EMERGENCY → 不阻断但标记提醒，跳过LLM
        3. 规则层未命中 → LLM层统一改写+深度安全审查
        """
        history = history or []

        try:
            rule_result = self._rule_filter.check(message)
            if rule_result.is_blocked:
                return InputSafetyResult(
                    level=rule_result.level,
                    needs_crisis_response=rule_result.level == 'CRISIS',
                    needs_emergency_reminder=False,
                    crisis_response=rule_result.crisis_response,
                    matched_keywords=rule_result.matched_keywords,
                    is_blocked=True,
                    block_reason=rule_result.block_reason,
                )
            if rule_result.needs_emergency_reminder:
                return InputSafetyResult(
                    level='EMERGENCY',
                    needs_crisis_response=False,
                    needs_emergency_reminder=True,
                    is_blocked=False,
                    matched_keywords=rule_result.matched_keywords,
                )
        except Exception as e:
            logger.error("RULE_FILTER_FALLBACK | reason=%s", str(e), exc_info=True)

        try:
            llm_result = self.rewriter.rewrite_and_check(message, history)
            if llm_result.is_blocked:
                return InputSafetyResult(
                    level='BLOCKED',
                    is_blocked=True,
                    block_reason=llm_result.safety_reason or 'LLM安全审查拦截',
                )
        except Exception as e:
            logger.error("LLM_SAFETY_FALLBACK | reason=%s", str(e), exc_info=True)

        return InputSafetyResult(level='NORMAL')

    def validate_output_safety(self, text: str):
        from .strategies import OutputSafetyValidator, OutputSafetyResult
        try:
            validator = OutputSafetyValidator()
            return validator.validate(text)
        except Exception as e:
            logger.error("OUTPUT_SAFETY_FALLBACK | reason=%s", str(e), exc_info=True)
            return OutputSafetyResult(level='PASS')

    def save_user_message(
        self,
        patient: PatientProfile,
        conversation_id: Optional[str],
        message_content: str,
        session_key: Optional[str] = None,
        selected_departments: Optional[List[Department]] = None,
    ) -> Optional[Conversation]:
        conversation = self.get_or_create_conversation(
            patient, conversation_id, message_content, session_key,
            selected_departments=selected_departments
        )
        if not conversation:
            return None
        Message.objects.create(
            conversation=conversation,
            sender=Message.Sender.USER,
            content=message_content
        )
        return conversation

    def create_conversation(
        self,
        patient: PatientProfile,
        first_message: str,
    ) -> Conversation:
        if not patient:
            raise ValueError("Patient is required to create a conversation")
        from .services.conversation_service import ConversationService
        return Conversation.objects.create(
            patient=patient,
            title=ConversationService.generate_title(first_message)
        )

    def get_or_create_conversation(
        self,
        patient: PatientProfile,
        conversation_id: Optional[str],
        message_content: str,
        session_key: Optional[str] = None,
        selected_departments: Optional[List[Department]] = None,
    ) -> Optional[Conversation]:
        from .services.conversation_service import ConversationService
        generated_title = ConversationService.generate_title(message_content)

        if patient:
            if conversation_id:
                return get_object_or_404(
                    Conversation, id=conversation_id, patient=patient
                )
            recent = Conversation.objects.filter(
                patient=patient
            ).prefetch_related('messages').order_by('-created_at').first()
            if recent and recent.messages.count() == 0:
                if selected_departments and not recent.departments.exists():
                    recent.departments.set(selected_departments)
                return recent
            conv = Conversation.objects.create(
                patient=patient,
                title=generated_title
            )
            if selected_departments:
                conv.departments.set(selected_departments)
            return conv
        elif session_key:
            if conversation_id:
                try:
                    return Conversation.objects.get(
                        id=conversation_id, patient__isnull=True, session_key=session_key
                    )
                except Conversation.DoesNotExist:
                    return None
            recent = Conversation.objects.filter(
                patient__isnull=True, session_key=session_key
            ).prefetch_related('messages').order_by('-created_at').first()
            if recent and recent.messages.count() == 0:
                if selected_departments and not recent.departments.exists():
                    recent.departments.set(selected_departments)
                return recent
            conv = Conversation.objects.create(
                patient=None,
                session_key=session_key,
                title=generated_title
            )
            if selected_departments:
                conv.departments.set(selected_departments)
            return conv
        return None

    def validate_department_lock(
        self,
        conversation: Conversation,
        requested_dept_ids: List[int],
    ) -> List[Department]:
        locked_dept_ids = set(
            conversation.departments.values_list('id', flat=True)
        )
        if not locked_dept_ids:
            return list(Department.objects.filter(id__in=requested_dept_ids))

        return list(Department.objects.filter(id__in=locked_dept_ids))

    def _get_health_data_share_enabled(self, patient: Optional[PatientProfile]) -> bool:
        return ConversationService.get_health_data_share_enabled(patient)

    def get_ai_response_stream(
        self,
        message: str,
        patient: Optional[PatientProfile],
        selected_departments: List[Department],
        conversation_id: Optional[str] = None,
    ):
        if not self.rag_svc:
            yield "AI 健康助手服务暂时不可用，请稍后再试。"
            return

        health_data_share_enabled = self._get_health_data_share_enabled(patient)

        history = []
        if conversation_id:
            history = ConversationService.get_recent_messages(
                conversation_id, patient=patient
            )

        query_for_retrieval = self._get_rewrite_query(message, history)

        system_prompt, relevant_chunks = self.rag_svc.rag_retrieve(
            query_for_retrieval,
            patient,
            selected_departments=list(selected_departments) if selected_departments else None,
            health_data_share_enabled=health_data_share_enabled
        )

        if not system_prompt:
            rejection_msg = "未找到相关健康知识，建议您换个问题或咨询医生获取专业建议。"
            for token in rejection_msg:
                yield token
            return

        for token in self.llm_client.generate_stream(system_prompt, query_for_retrieval):
            yield token

    def rag_retrieve_and_llm_stream(
        self,
        message: str,
        patient: Optional[PatientProfile],
        selected_departments: List[Department],
        conversation_id: Optional[str] = None,
    ) -> Tuple[Generator[str, None, None], list, bool, int, Optional[str]]:
        if not self.rag_svc:
            return (
                (msg for msg in ["抱歉，系统暂时繁忙，请稍后再试。"]),
                [],
                False,
                0,
                'system_unavailable',
            )

        health_data_share_enabled = self._get_health_data_share_enabled(patient)

        history = []
        if conversation_id:
            history = ConversationService.get_recent_messages(
                conversation_id, patient=patient
            )

        query_for_retrieval = self._get_rewrite_query(message, history)

        try:
            system_prompt, relevant_chunks = self.rag_svc.rag_retrieve(
                query_for_retrieval,
                patient,
                selected_departments=list(selected_departments) if selected_departments else None,
                health_data_share_enabled=health_data_share_enabled
            )
        except Exception as e:
            logger.error("RAG retrieve failed: %s", str(e))
            return (
                (msg for msg in ["抱歉，系统暂时繁忙，请稍后再试。"]),
                [],
                False,
                0,
                'system_unavailable',
            )

        if not system_prompt:
            rejection_msg = "未找到相关健康知识，建议您换个问题或咨询医生获取专业建议。"
            return (msg for msg in [rejection_msg]), [], False, 0, 'no_knowledge'

        should_warn = False
        estimated_tokens = 0
        if conversation_id:
            should_warn, estimated_tokens = ConversationService.check_context_warning(conversation_id, patient)

        return self.llm_client.generate_stream(system_prompt, query_for_retrieval), relevant_chunks, should_warn, estimated_tokens, None

    def _get_rewrite_query(self, message: str, history: list) -> str:
        """获取改写后的查询，复用UnifiedSafetyRewriter的改写能力"""
        if not history:
            return message
        try:
            result = self.rewriter.rewrite_and_check(message, history)
            return result.rewrite or message
        except Exception as e:
            logger.error("QUERY_REWRITE_FALLBACK | reason=%s", str(e), exc_info=True)
            return message

    def save_ai_message(
        self,
        conversation: Optional[Conversation],
        answer: str,
        relevant_chunks: list,
        is_streaming: bool = False,
    ) -> Optional[Message]:
        if not conversation:
            return None

        ai_message = Message.objects.create(
            conversation=conversation,
            sender=Message.Sender.AI,
            content=answer,
            is_streaming=is_streaming,
        )
        if relevant_chunks:
            ai_message.reference_chunks.set(relevant_chunks)
        return ai_message

    def save_safety_message(
        self,
        conversation: Optional[Conversation],
        content: str,
        safety_level: str,
        safety_reason: str,
    ) -> Optional[Message]:
        if not conversation:
            return None
        processing_result = Message.ProcessingResult.CRISIS
        if safety_level in ('BLOCKED', 'INTERCEPTED'):
            processing_result = Message.ProcessingResult.INTERCEPTED
        return Message.objects.create(
            conversation=conversation,
            sender=Message.Sender.AI,
            content=content,
            is_streaming=False,
            is_safety_flagged=True,
            safety_flag_level=safety_level,
            safety_flag_reason=safety_reason,
            processing_result=processing_result,
        )

    def mark_message_safety(
        self,
        message: Message,
        level: str,
        reason: str,
        original_content: str = '',
    ):
        message.is_safety_flagged = True
        message.safety_flag_level = level
        message.safety_flag_reason = reason
        if original_content:
            message.original_content = original_content
        message.save(update_fields=[
            'is_safety_flagged', 'safety_flag_level',
            'safety_flag_reason', 'original_content',
        ])

    def update_message_content(self, message: Message, content: str):
        message.content = content
        message.save(update_fields=['content'])

    def finalize_ai_message(
        self,
        message: Message,
        answer: str,
        relevant_chunks: list,
    ):
        from apps.chat.services.conversation_service import ConversationService
        ConversationService.finalize_ai_message(message, answer, relevant_chunks)
