"""
LLM Client - 团队D韧性治理重构版

基于 Guide.md §11 AI与实时能力治理规范：
- 明确入口、明确上下文来源、明确风控规则
- 明确异常处理与降级方案
- 明确输出边界与责任归属
- 超时控制、重试策略、降级处理
"""
import re
import time
import logging
from typing import Protocol, Generator

from apps.config.services import AIConfigService, SafetyConfigService

logger = logging.getLogger(__name__)

LLM_RETRY_DELAY = 2


def _get_llm_timeout():
    return AIConfigService.get_llm_config().get('timeout', 60)


def _get_llm_max_retries():
    return AIConfigService.get_llm_config().get('max_retries', 3)


def _get_llm_fallback():
    return SafetyConfigService.get_rejection_message()


def _filter_think_tokens(content, state):
    """过滤 LLM 思考标签（如 MiniMax）。

    状态机：state 是 dict，键 'in_think' 追踪是否在思考块内。
    返回 (filtered_content, updated_state)。
    """
    if not content:
        return '', state

    result = []
    pos = 0
    in_think = state.get('in_think', False)

    while pos < len(content):
        if in_think:
            close_idx = content.find('</think>', pos)
            if close_idx != -1:
                in_think = False
                pos = close_idx + len('</think>')
            else:
                pos = len(content)
        else:
            open_idx = content.find('<think>', pos)
            if open_idx != -1:
                if open_idx > pos:
                    result.append(content[pos:open_idx])
                in_think = True
                pos = open_idx + len('<think>')
            else:
                result.append(content[pos:])
                pos = len(content)

    state['in_think'] = in_think
    return ''.join(result), state


class LLMClientStrategy(Protocol):
    def generate(self, system_prompt: str, query: str) -> str: ...
    def generate_stream(self, system_prompt: str, query: str) -> Generator[str, None, None]: ...


class ResilientLLMClientMixin:
    """LLM客户端韧性增强：重试、超时、降级"""

    def _call_with_retry(self, func, *args, max_retries=None, **kwargs) -> str:
        max_retries = max_retries or _get_llm_max_retries()
        last_error = None

        for attempt in range(max_retries + 1):
            try:
                return func(*args, **kwargs)
            except Exception as e:
                if self._is_retryable(e):
                    last_error = e
                    if attempt < max_retries:
                        delay = LLM_RETRY_DELAY * (2 ** attempt)
                        logger.warning(
                            "LLM_RETRY | attempt=%d | max_retries=%d | delay=%ds | error=%s",
                            attempt + 1, max_retries, delay, str(e),
                        )
                        time.sleep(delay)
                    else:
                        logger.error(
                            "LLM_EXHAUSTED_RETRIES | attempts=%d | error=%s",
                            max_retries + 1, str(e),
                        )
                else:
                    logger.error("LLM_UNRETRYABLE_ERROR | error=%s", str(e))
                    raise

        logger.error("LLM_FALLBACK | reason=exhausted_retries")
        return _get_llm_fallback()

    def _is_retryable(self, exc: Exception) -> bool:
        import requests
        if isinstance(exc, requests.exceptions.HTTPError):
            response = getattr(exc, 'response', None)
            if response is not None:
                status_code = getattr(response, 'status_code', 0)
                if status_code >= 500 or status_code == 429:
                    return True
            return False
        return isinstance(exc, (ConnectionError, TimeoutError, OSError))

    def generate_with_fallback(self, system_prompt: str, query: str) -> str:
        import requests

        def _do_generate():
            url = self._build_url('/chat/completions')
            headers = {
                "Authorization": f"Bearer {self._get_api_key()}",
                "Content-Type": "application/json"
            }
            payload = {
                "model": self._get_model_name(),
                "messages": [
                    {"role": "system", "content": system_prompt},
                    {"role": "user", "content": query}
                ],
                "temperature": self._temperature
            }

            response = requests.post(url, json=payload, headers=headers, timeout=self._timeout)
            response.raise_for_status()
            data = response.json()
            raw = data['choices'][0]['message']['content']
            filtered, _ = _filter_think_tokens(raw, {'in_think': False})
            return filtered

        return self._call_with_retry(_do_generate)

    def generate_stream_with_fallback(self, system_prompt: str, query: str):
        import requests

        url = self._build_url('/chat/completions')
        headers = {
            "Authorization": f"Bearer {self._get_api_key()}",
            "Content-Type": "application/json"
        }
        payload = {
            "model": self._get_model_name(),
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": query}
            ],
            "temperature": self._temperature,
            "stream": True
        }

        try:
            response = requests.post(url, json=payload, headers=headers, timeout=self._timeout, stream=True)
            response.raise_for_status()

            import json
            think_state = {'in_think': False}
            for line in response.iter_lines():
                if not line:
                    continue
                if not line.startswith(b'data: '):
                    continue
                data_str = line[len(b'data: '):].decode('utf-8', errors='replace')
                if data_str == '[DONE]':
                    break
                try:
                    data = json.loads(data_str)
                except json.JSONDecodeError:
                    continue
                choices = data.get('choices', [])
                if not choices:
                    continue
                delta = choices[0].get('delta', {})
                content = delta.get('content')
                if content:
                    filtered, think_state = _filter_think_tokens(content, think_state)
                    if filtered:
                        yield filtered
        except Exception as e:
            logger.error("LLM_STREAM_ERROR | error=%s", str(e))
            yield _get_llm_fallback()


class OpenAICompatibleLLMClient(ResilientLLMClientMixin):

    def __init__(
        self,
        base_url: str = None,
        api_key: str = None,
        model_name: str = None,
        timeout: int = None,
        temperature: float = 0.1,
    ):
        config = None
        if not (base_url and api_key and model_name):
            try:
                config = AIConfigService.get_llm_config()
            except ValueError:
                pass
        self._base_url = base_url or (config.get('base_url') if config else '')
        self._api_key = api_key or (config.get('api_key') if config else '')
        self._model_name = model_name or (config.get('model_name') if config else '')
        self._timeout = timeout or (config.get('timeout', 60) if config else 60)
        self._temperature = temperature or (config.get('temperature', 0.1) if config else 0.1)

    def _get_base_url(self) -> str:
        return self._base_url

    def _get_api_key(self) -> str:
        return self._api_key

    def _get_model_name(self) -> str:
        return self._model_name

    def _build_url(self, path: str) -> str:
        base = self._get_base_url().rstrip('/')
        if '/chat/completions' in base:
            return base
        return f"{base}{path}"

    def generate(self, system_prompt: str, query: str) -> str:
        return self.generate_with_fallback(system_prompt, query)

    def generate_stream(self, system_prompt: str, query: str):
        yield from self.generate_stream_with_fallback(system_prompt, query)


class AnthropicLLMClient(ResilientLLMClientMixin):
    """Anthropic Claude API client."""

    def __init__(
        self,
        base_url: str = None,
        api_key: str = None,
        model_name: str = None,
        timeout: int = None,
        temperature: float = 0.1,
        max_tokens: int = 1024,
    ):
        config = AIConfigService.get_llm_config()
        self._base_url = base_url or config.get('base_url')
        self._api_key = api_key or config.get('api_key')
        self._model_name = model_name or config.get('model_name')
        self._timeout = timeout or config.get('timeout', 60)
        self._temperature = temperature or config.get('temperature', 0.1)
        self._max_tokens = max_tokens or config.get('max_tokens', 1024)

    def _get_base_url(self) -> str:
        return self._base_url

    def _get_api_key(self) -> str:
        return self._api_key

    def _get_model_name(self) -> str:
        return self._model_name

    def _build_url(self, path: str) -> str:
        base = self._get_base_url().rstrip('/')
        if base.endswith('/messages'):
            return base
        return f"{base}/messages"

    def generate(self, system_prompt: str, query: str) -> str:
        import requests

        def _do_generate():
            url = self._build_url('/messages')
            headers = {
                "Authorization": f"Bearer {self._get_api_key()}",
                "Content-Type": "application/json",
                "anthropic-version": "2023-06-01"
            }
            payload = {
                "model": self._get_model_name(),
                "system": system_prompt,
                "messages": [
                    {"role": "user", "content": query}
                ],
                "temperature": self._temperature,
                "max_tokens": self._max_tokens
            }

            response = requests.post(url, json=payload, headers=headers, timeout=self._timeout)
            response.raise_for_status()
            data = response.json()
            return data['content'][0]['text']

        return self._call_with_retry(_do_generate)

    def generate_stream(self, system_prompt: str, query: str):
        import requests
        import json

        url = self._build_url('/messages')
        headers = {
            "Authorization": f"Bearer {self._get_api_key()}",
            "Content-Type": "application/json",
            "anthropic-version": "2023-06-01"
        }
        payload = {
            "model": self._get_model_name(),
            "system": system_prompt,
            "messages": [
                {"role": "user", "content": query}
            ],
            "temperature": self._temperature,
            "max_tokens": self._max_tokens,
            "stream": True
        }

        try:
            response = requests.post(url, json=payload, headers=headers, timeout=self._timeout, stream=True)
            response.raise_for_status()

            for line in response.iter_lines():
                if not line:
                    continue
                if not line.startswith(b'data: '):
                    continue
                data_str = line[len(b'data: '):].decode('utf-8', errors='replace')
                try:
                    data = json.loads(data_str)
                except json.JSONDecodeError:
                    continue
                if data.get('type') == 'content_block_delta':
                    delta = data.get('delta', {})
                    text = delta.get('text')
                    if text:
                        yield text
                elif data.get('type') == 'message_stop':
                    break
        except Exception as e:
            logger.error("ANTHROPIC_STREAM_ERROR | error=%s", str(e))
            yield _get_llm_fallback()


def get_llm_client():
    config = AIConfigService.get_llm_config()
    provider = config.get('provider', 'openai').lower()
    if provider == 'anthropic':
        return AnthropicLLMClient(
            base_url=config.get('base_url'),
            api_key=config.get('api_key'),
            model_name=config.get('model_name'),
            timeout=config.get('timeout'),
            temperature=config.get('temperature'),
            max_tokens=config.get('max_tokens'),
        )
    return OpenAICompatibleLLMClient(
        base_url=config.get('base_url'),
        api_key=config.get('api_key'),
        model_name=config.get('model_name'),
        timeout=config.get('timeout'),
        temperature=config.get('temperature'),
    )


def call_llm(system_prompt: str, query: str) -> str:
    client = get_llm_client()
    return client.generate(system_prompt, query)
