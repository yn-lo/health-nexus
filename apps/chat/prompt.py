import json

from .prompt_config import get_prompt_template


def build_patient_context(patient_profile, health_data_share_enabled=True) -> str:
    if not patient_profile:
        return "未知患者"

    if not health_data_share_enabled:
        return "未知患者"

    parts = []

    age = patient_profile.age
    if age:
        age_bracket = f"{(age // 10) * 10}多岁"
        parts.append(f"{age_bracket} {patient_profile.get_gender_display()}")
    else:
        parts.append(patient_profile.get_gender_display())

    if patient_profile.medical_history_summary:
        parts.append(f"既往病史：{patient_profile.medical_history_summary}")
    if patient_profile.allergies_summary:
        parts.append(f"过敏史：{patient_profile.allergies_summary}")
    if patient_profile.latest_vitals:
        parts.append(f"最新体征：{json.dumps(patient_profile.latest_vitals, ensure_ascii=False)}")

    return "，".join(parts)


def build_context_string(chunks) -> str:
    parts = []
    for chunk in chunks:
        entry = f"参考资料 [{chunk.article.title}]:\n{chunk.content_text}"
        if getattr(chunk.article, 'review_overdue', False):
            entry += "\n[注意：该内容待医生复核，请以最新医嘱为准]"
        parts.append(entry)
    return "\n\n".join(parts)


def build_system_prompt(patient_info: str, context_str: str) -> str:
    template = get_prompt_template()
    return template.build_system_prompt(patient_info, context_str)


def build_rewrite_prompt(current_query: str, history: list) -> str:
    """
    将对话历史格式化为查询改写提示词
    
    Args:
        current_query: 用户当前输入的问题
        history: 历史消息对列表，格式为 [(user_msg, ai_msg), ...]
    
    Returns:
        格式化后的提示词字符串
    """
    parts = ["你是一个查询改写助手。请根据以下对话历史，将用户的当前问题改写为一个独立完整的问题。"]
    
    if history:
        parts.append("对话历史：")
        for user_msg, ai_msg in history:
            user_content = getattr(user_msg, 'content', str(user_msg))
            ai_content = getattr(ai_msg, 'content', str(ai_msg))
            parts.append(f"用户：{user_content}")
            parts.append(f"AI：{ai_content}")
        parts.append("")
    
    parts.append(f"用户当前问题：{current_query}")
    parts.append("")
    parts.append("请只输出改写后的问题，不要添加任何解释。")
    
    return "\n".join(parts)
