"""
Chat Response Strategies - 聊天响应策略

定义不同响应格式的策略：
- SSEResponseStrategy: Server-Sent Events 流式响应
"""
from abc import ABC, abstractmethod
import json
import markdown
from django.template.loader import render_to_string


class ChatResponseStrategy(ABC):
    """聊天响应策略抽象基类"""

    @abstractmethod
    def generate_user_message(self, html: str) -> str:
        pass

    @abstractmethod
    def generate_status(self, text: str) -> str:
        pass

    @abstractmethod
    def generate_sensitive_response(self, html: str) -> str:
        pass

    @abstractmethod
    def generate_token(self, token: str) -> str:
        pass

    @abstractmethod
    def generate_ai_message(self, html: str, message_id, reference_chunks: list, feedback: int) -> str:
        pass

    @abstractmethod
    def generate_complete(self) -> str:
        pass

    @abstractmethod
    def generate_context_warning(self, estimated_tokens: int) -> str:
        pass

    @abstractmethod
    def generate_error(self, error_type: str, message: str, suggestion: str = None) -> str:
        pass

    @abstractmethod
    def get_content_type(self) -> str:
        pass


class ChatErrorType:
    """错误类型枚举"""
    SYSTEM_UNAVAILABLE = 'system_unavailable'
    NO_KNOWLEDGE = 'no_knowledge'
    RATE_LIMITED = 'rate_limited'
    BLOCKED = 'blocked'
    CRISIS = 'crisis'


class ChatErrorMessages:
    """错误消息规范"""
    SYSTEM_UNAVAILABLE = "AI 健康助手服务暂时不可用，请稍后再试"
    NO_KNOWLEDGE = "未找到相关健康知识，建议您换一种方式提问或咨询医生获取专业建议"
    RATE_LIMITED = "今日免费咨询次数已达上限，请明天再来"
    BLOCKED = "请求无法处理，请修改后重试"
    CRISIS = "您描述的症状可能需要紧急处理，请立即联系您的主治医生或拨打急救电话"


class SSEResponseStrategy(ChatResponseStrategy):
    """SSE (Server-Sent Events) 响应策略"""

    def generate_user_message(self, html: str) -> str:
        return f"data: {json.dumps({'type': 'user_message', 'html': html})}\n\n"

    def generate_status(self, text: str) -> str:
        return f"data: {json.dumps({'type': 'status', 'text': text})}\n\n"

    def generate_sensitive_response(self, html: str) -> str:
        final_html = render_to_string('chat/partials/ai_message.html', {
            'content': html,
            'message_id': None,
            'reference_chunks': [],
            'is_sensitive': True,
            'feedback': 0,
        })
        return f"data: {json.dumps({'type': 'ai_message', 'html': final_html})}\n\n"

    def generate_token(self, token: str) -> str:
        escaped_token = json.dumps({'type': 'ai_token', 'token': token})
        return f"data: {escaped_token}\n\n"

    def generate_ai_message(self, html: str, message_id, reference_chunks: list, feedback: int) -> str:
        final_html = render_to_string('chat/partials/ai_message.html', {
            'content': html,
            'message_id': message_id,
            'reference_chunks': reference_chunks,
            'is_sensitive': False,
            'feedback': feedback,
        })
        return f"data: {json.dumps({'type': 'ai_message', 'html': final_html})}\n\n"

    def generate_complete(self) -> str:
        return f"data: {json.dumps({'type': 'complete'})}\n\n"

    def generate_context_warning(self, estimated_tokens: int) -> str:
        return f"data: {json.dumps({'type': 'context_warning', 'estimated_tokens': estimated_tokens})}\n\n"

    def generate_error(self, error_type: str, message: str, suggestion: str = None) -> str:
        error_data = {
            'type': 'error',
            'error_type': error_type,
            'message': message,
        }
        if suggestion:
            error_data['suggestion'] = suggestion
        return f"data: {json.dumps(error_data, ensure_ascii=False)}\n\n"

    def get_content_type(self) -> str:
        return 'text/event-stream'
