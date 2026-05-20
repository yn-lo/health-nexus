"""
Chat Send Service - 聊天发送核心业务逻辑

封装完整的聊天发送流程，包括：
- 参数解析和验证
- Orchestrator 调用和患者信息解析
- 科室权限过滤
- 缓存锁机制（防重复提交）
- 敏感词检测和处理
- 用户消息保存
- AI 响应生成
- AI 消息保存和最终化
"""
import logging
from typing import Optional, List, Tuple, Generator, Any

from django.core.cache import cache
from django.template.loader import render_to_string

from apps.base.models import Department
from apps.care.models import PatientProfile
from apps.chat.models import Conversation, Message
from apps.chat.orchestrator import ChatSendOrchestrator

logger = logging.getLogger(__name__)


class ChatSendResult:
    """聊天发送结果数据类"""

    def __init__(
        self,
        user_message_html: str,
        system_hint: str,
        is_sensitive: bool,
        sensitive_result: Optional[str] = None,
        conversation: Optional[Conversation] = None,
        patient: Optional[PatientProfile] = None,
        selected_departments: Optional[List[Department]] = None,
        relevant_chunks: Optional[List] = None,
    ):
        self.user_message_html = user_message_html
        self.system_hint = system_hint
        self.is_sensitive = is_sensitive
        self.sensitive_result = sensitive_result
        self.conversation = conversation
        self.patient = patient
        self.selected_departments = selected_departments or []
        self.relevant_chunks = relevant_chunks or []


class ChatSendService:
    """聊天发送服务：封装完整的聊天发送流程"""

    LOCK_TIMEOUT = 30

    def __init__(self, orchestrator=None):
        self._orchestrator = orchestrator or ChatSendOrchestrator()

    @property
    def orchestrator(self):
        return self._orchestrator

    def send_message(
        self,
        message: str,
        user,
        conversation_id: Optional[str],
        selected_dept_ids: List[str],
        session_key: Optional[str] = None,
    ) -> Tuple[Optional[ChatSendResult], str]:
        if not message:
            return None, ""

        patient = self._orchestrator.resolve_patient(user)
        selected_departments = self._orchestrator.filter_departments(
            selected_dept_ids, patient
        )

        lock_key = self._get_lock_key(patient, conversation_id, session_key)
        if self._is_locked(lock_key):
            return None, lock_key

        self._set_lock(lock_key)

        try:
            input_safety = self._orchestrator.classify_input_safety(message)
            is_sensitive = input_safety.level == 'CRISIS'
            sensitive_result = input_safety.crisis_response if is_sensitive else None

            conversation = self._orchestrator.save_user_message(
                patient, conversation_id, message, session_key,
                selected_departments=selected_departments
            )
            if conversation and conversation_id is None and patient:
                new_lock_key = self._get_lock_key(patient, str(conversation.id), session_key)
                self._set_lock(new_lock_key)
                lock_key = new_lock_key

            user_message_html = self._render_user_message(message)
            system_hint = self._generate_system_hint(selected_departments)

            result = ChatSendResult(
                user_message_html=user_message_html,
                system_hint=system_hint,
                is_sensitive=is_sensitive,
                sensitive_result=sensitive_result,
                conversation=conversation,
                patient=patient,
                selected_departments=selected_departments,
            )

            return result, lock_key

        except Exception:
            if lock_key:
                self._release_lock(lock_key)
            raise

    def get_ai_response_stream(
        self,
        message: str,
        patient: Optional[PatientProfile],
        selected_departments: List[Department],
        conversation_id: Optional[str] = None,
    ) -> Tuple[Generator[str, None, None], List[Any], bool, int, Optional[str]]:
        """
        获取 AI 流式响应

        返回: (token生成器, relevant_chunks, should_warn, estimated_tokens, error_type)
        """
        return self._orchestrator.rag_retrieve_and_llm_stream(
            message, patient, selected_departments, conversation_id
        )

    def save_ai_message_streaming(
        self,
        conversation: Optional[Conversation],
        relevant_chunks: List[Any],
    ) -> Optional[Message]:
        """创建 streaming 占位消息"""
        if not conversation:
            return None
        return self._orchestrator.save_ai_message(
            conversation, "", relevant_chunks, is_streaming=True
        )

    def finalize_ai_message(
        self,
        ai_message: Message,
        answer: str,
        relevant_chunks: List[Any],
    ):
        """完成流式生成，更新消息内容"""
        if ai_message:
            self._orchestrator.finalize_ai_message(
                ai_message, answer, relevant_chunks
            )

    def save_ai_message_sync(
        self,
        conversation: Optional[Conversation],
        answer: str,
        relevant_chunks: List[Any],
    ) -> Optional[Message]:
        """保存 AI 消息（非流式）"""
        if not conversation:
            return None
        return self._orchestrator.save_ai_message(
            conversation, answer, relevant_chunks
        )

    def release_lock(self, lock_key: str):
        """释放缓存锁"""
        if lock_key:
            self._release_lock(lock_key)

    def _get_lock_key(self, patient: Optional[PatientProfile], conversation_id: Optional[str], session_key: Optional[str] = None) -> str:
        if patient:
            return f"chat_pending:{patient.id}:{conversation_id or 'new'}"
        session_id = session_key or 'unknown'
        return f"chat_pending:anon:{session_id}:{conversation_id or 'new'}"

    def _is_locked(self, lock_key: str) -> bool:
        """检查是否被锁定"""
        if not lock_key:
            return False
        return cache.get(lock_key) is not None

    def _set_lock(self, lock_key: str):
        """设置缓存锁"""
        if lock_key:
            cache.set(lock_key, True, timeout=self.LOCK_TIMEOUT)

    def _release_lock(self, lock_key: str):
        """释放缓存锁"""
        if lock_key:
            cache.delete(lock_key)

    def _render_user_message(self, message: str) -> str:
        """渲染用户消息 HTML"""
        return render_to_string('chat/partials/user_message.html', {
            'content': message,
        })

    def _generate_system_hint(self, selected_departments: List[Department]) -> str:
        """生成系统提示"""
        if selected_departments:
            dept_names = '、'.join([dept.name for dept in selected_departments])
            return f'正在从以下科室检索: {dept_names}'
        return '正在从公共知识库检索'
