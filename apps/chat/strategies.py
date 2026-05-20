"""
两层安全防线策略定义

输入侧：RuleBasedInputFilter（规则层快速拦截）+ UnifiedSafetyRewriter（LLM层深度审查）
输出侧：OutputSafetyValidator（校验，分级响应）
"""

import re
import logging
from dataclasses import dataclass, field
from typing import Optional, List

from apps.config.services import SafetyConfigService

logger = logging.getLogger(__name__)


@dataclass
class InputSafetyResult:
    level: str = 'NORMAL'
    needs_crisis_response: bool = False
    needs_emergency_reminder: bool = False
    crisis_response: Optional[str] = None
    matched_keywords: List[str] = field(default_factory=list)
    is_blocked: bool = False
    block_reason: str = ''


@dataclass
class RuleFilterResult:
    is_blocked: bool = False
    level: str = 'PASS'
    crisis_response: Optional[str] = None
    needs_emergency_reminder: bool = False
    matched_keywords: List[str] = field(default_factory=list)
    block_reason: str = ''


class RuleBasedInputFilter:
    """规则层输入审查：基于关键词快速拦截危机/紧急/注入请求

    设计原则：零延迟、100%命中已知危险模式、可解释。
    未命中时放行到LLM层做深度审查。
    """

    DEFAULT_CRISIS_KEYWORDS = ['自杀', '自残', '不想活', '想死', '结束生命', '跳楼', '割腕']
    DEFAULT_EMERGENCY_KEYWORDS = ['胸痛', '呼吸困难', '大出血', '昏迷', '抽搐', '剧烈疼痛']
    DEFAULT_INJECTION_PATTERNS = [
        r'忽略.{0,5}(之前|上文|前面).{0,5}(指令|提示|要求)',
        r'你(现在|目前)是.{0,10}(没有限制|无限制|不受约束)',
        r'忘记.{0,5}(身份|角色|设定)',
        r'系统提示',
        r'DAN模式',
        r'jailbreak',
    ]
    NEGATION_WORDS = ['没有', '没', '不', '无', '未']

    def _get_crisis_keywords(self) -> List[str]:
        try:
            words = SafetyConfigService.get_sensitive_words(category='SUICIDE')
            if words:
                return words
        except Exception:
            logger.warning("CRISIS_KEYWORDS_FALLBACK | reason=db_read_failed")
        return self.DEFAULT_CRISIS_KEYWORDS

    def _get_emergency_keywords(self) -> List[str]:
        try:
            words = SafetyConfigService.get_sensitive_words(category='EMERGENCY')
            if words:
                return words
        except Exception:
            logger.warning("EMERGENCY_KEYWORDS_FALLBACK | reason=db_read_failed")
        return self.DEFAULT_EMERGENCY_KEYWORDS

    def _get_injection_patterns(self) -> List[str]:
        try:
            patterns = SafetyConfigService.get_sensitive_words(category='INJECTION')
            if patterns:
                return patterns
        except Exception:
            logger.warning("INJECTION_PATTERNS_FALLBACK | reason=db_read_failed")
        return self.DEFAULT_INJECTION_PATTERNS

    def check(self, query: str) -> RuleFilterResult:
        query_lower = query.lower()
        result = RuleFilterResult()

        for keyword in self._get_crisis_keywords():
            if keyword in query_lower:
                if not self._has_negation_before(query_lower, keyword):
                    result.is_blocked = True
                    result.level = 'CRISIS'
                    result.matched_keywords = [keyword]
                    result.crisis_response = (
                        "我注意到您可能正在经历困难。"
                        "请立即联系心理援助热线：400-161-9995，"
                        "或拨打120寻求紧急帮助。您并不孤单。"
                    )
                    return result

        for keyword in self._get_emergency_keywords():
            if keyword in query_lower:
                if not self._has_negation_before(query_lower, keyword):
                    result.is_blocked = False
                    result.level = 'EMERGENCY'
                    result.matched_keywords = [keyword]
                    result.needs_emergency_reminder = True
                    return result

        for pattern in self._get_injection_patterns():
            if re.search(pattern, query_lower):
                result.is_blocked = True
                result.level = 'INJECTION'
                result.block_reason = '检测到Prompt注入尝试'
                return result

        return result

    def _has_negation_before(self, query: str, keyword: str) -> bool:
        idx = query.find(keyword)
        if idx <= 0:
            return False
        window_start = max(0, idx - 5)
        prefix = query[window_start:idx]
        for neg in self.NEGATION_WORDS:
            neg_idx = prefix.rfind(neg)
            if neg_idx >= 0:
                gap = len(prefix) - neg_idx - len(neg)
                if gap <= 1:
                    return True
        return False


@dataclass
class OutputSafetyResult:
    level: str = 'PASS'
    should_block: bool = False
    needs_disclaimer: bool = False
    disclaimer_text: Optional[str] = None
    matched_pattern: Optional[str] = None


class OutputSafetyValidator:
    """输出侧安全校验：检测AI是否越权

    分级响应：
    - BLOCKED：AI越权（下诊断、开处方、建议停药/不治疗）→ 替换为安全话术
    - WARNING：AI提及用药/剂量 → 追加免责声明
    - PASS：正常输出 → 放行
    """

    DEFAULT_BLOCKED_PATTERNS = [
        (r'你(得了|患有|确诊为)', 'AI越权诊断'),
        (r'每天(吃|服)\d+(片|粒|mg|毫升)', 'AI越权开处方'),
        (r'(不用|不需要)(治疗|就医|去医院|看医生)', 'AI延误就医'),
        (r'(可以|应该|请)(停药|停止用药)', 'AI建议停药'),
    ]

    DEFAULT_WARNING_PATTERNS = [
        (r'建议?服用', 'AI提及用药'),
        (r'建议?使用.+药', 'AI提及用药'),
        (r'(常用|推荐|标准)剂量', 'AI提及剂量'),
        (r'\d+[-~]\d+\s*mg', 'AI提及具体剂量'),
    ]

    PASS_EXCEPTIONS = [
        r'无法?确诊',
        r'不能?为您?(开处方|诊断|确诊)',
        r'请(不要|勿)(自行|擅自)(停药|服用|用药)',
        r'如果(出现|发生).+请(立即|及时|尽快)(拨打|就医|就诊|去医院)',
        r'(建议|请)咨询(专业|主治)?医生',
        r'诊断标准是',
        r'确诊为.+的(患者|人)',
        r'如果确诊为',
    ]

    MEDICATION_DISCLAIMER = '⚠️ 以上信息仅供参考，具体用药请遵医嘱。'

    BLOCKED_REPLACEMENT = '⚠️ 抱歉，以上回复可能存在不恰当的医疗建议，请以专业医生的意见为准。如需帮助，请及时就医。'

    def _get_blocked_patterns(self) -> List[tuple]:
        try:
            patterns = SafetyConfigService.get_dangerous_patterns()
            if patterns:
                return [(p, 'AI越权输出') for p in patterns]
        except Exception:
            logger.warning("SAFETY_CONFIG_FALLBACK | reason=blocked_patterns_load_failed")
        return self.DEFAULT_BLOCKED_PATTERNS

    def _get_warning_patterns(self) -> List[tuple]:
        try:
            rule = SafetyConfigService.get_safety_warning()
            if isinstance(rule, dict) and 'warning_patterns' in rule:
                patterns = rule['warning_patterns']
                if patterns:
                    return [(p, 'AI提及用药/剂量') for p in patterns]
        except Exception as e:
            logger.warning("WARNING_PATTERNS_FALLBACK | reason=%s", str(e), exc_info=True)
        return self.DEFAULT_WARNING_PATTERNS

    def _is_pass_exception(self, answer: str) -> bool:
        for pattern in self.PASS_EXCEPTIONS:
            if re.search(pattern, answer):
                return True
        return False

    def validate(self, answer: str) -> OutputSafetyResult:
        result = OutputSafetyResult()

        for pattern, reason in self._get_blocked_patterns():
            match = re.search(pattern, answer)
            if match:
                if self._is_pass_exception(answer):
                    matched_block = match.group()
                    for exc_pattern in self.PASS_EXCEPTIONS:
                        exc_match = re.search(exc_pattern, answer)
                        if exc_match and abs(exc_match.start() - match.start()) < len(answer) * 0.3:
                            result.level = 'PASS'
                            return result
                result.level = 'BLOCKED'
                result.should_block = True
                result.matched_pattern = f"{reason}: \"{match.group()}\""
                return result

        for pattern, reason in self._get_warning_patterns():
            match = re.search(pattern, answer)
            if match:
                if self._is_pass_exception(answer):
                    result.level = 'PASS'
                    return result
                result.level = 'WARNING'
                result.needs_disclaimer = True
                result.disclaimer_text = self.MEDICATION_DISCLAIMER
                result.matched_pattern = f"{reason}: \"{match.group()}\""
                return result

        result.level = 'PASS'
        return result
