"""
默认配置定义（Data Migration 数据源）

所有业务配置的默认值集中在此，用于：
1. Data Migration 首次安装时导入数据库
2. Admin 管理界面作为参考

配置已迁移到数据库管理，不再从 settings.py 读取。
"""


# LLM 服务默认配置
DEFAULT_LLM_PROVIDERS = [
    {
        'name': 'DeepSeek-V2.5',
        'api_key': 'sk-your-key',
        'base_url': 'https://api.siliconflow.cn/v1/',
        'model_name': 'deepseek-ai/DeepSeek-V2.5',
        'provider': 'openai',
        'timeout': 60,
        'max_retries': 3,
        'temperature': 0.1,
        'max_tokens': 1024,
        'description': '默认 LLM 服务配置',
    },
]

# Embedding 服务默认配置
DEFAULT_EMBEDDING_PROVIDERS = [
    {
        'name': 'BGE-m3',
        'api_key': 'sk-your-key',
        'base_url': 'https://api.siliconflow.cn/v1/',
        'model_name': 'BAAI/bge-m3',
        'provider': 'openai',
        'timeout': 60,
        'max_retries': 3,
        'dimensions': 1024,
        'description': '默认 Embedding 服务配置',
    },
]

# Rerank 服务默认配置
DEFAULT_RERANK_PROVIDERS = [
    {
        'name': 'BGE-reranker-v2-m3',
        'api_key': 'sk-your-key',
        'base_url': 'https://api.siliconflow.cn/v1/',
        'model_name': 'BAAI/bge-reranker-v2-m3',
        'provider': 'openai',
        'timeout': 60,
        'max_retries': 3,
        'top_n': 5,
        'description': '默认 Rerank 服务配置',
    },
]

# 敏感词默认列表
# 输入侧：仅保留自杀自残类（其他由 InputSafetyRouter 处理）
DEFAULT_SENSITIVE_WORDS = [
    ('自杀', 'SUICIDE'),
    ('自残', 'SUICIDE'),
    ('不想活', 'SUICIDE'),
    ('跳楼', 'SUICIDE'),
    ('割腕', 'SUICIDE'),
    ('吃药自杀', 'SUICIDE'),
]

# 安全规则默认值
# 输出侧：仅保留真正危险的句式（其他由 OutputSafetyValidator 处理）
DEFAULT_SAFETY_RULES = [
    {
        'rule_type': 'DANGEROUS_OUTPUT',
        'rule_key': 'dangerous_patterns',
        'rule_value': [
            '你得了', '你患有', '确诊为',
            '不用治疗', '自己会好',
            '可以停药', '不需要就医',
            '我诊断', '我的诊断是',
        ],
        'description': '危险 AI 输出模式检测（仅拦截真正越权的句式）',
    },
    {
        'rule_type': 'REJECTION_MESSAGE',
        'rule_key': 'medical_rejection',
        'rule_value': [
            '抱歉，我无法提供医疗诊断建议。如有健康问题，请咨询专业医生。',
            '作为 AI 助手，我不能为您诊断疾病。建议您尽快就医。',
        ],
        'description': '医疗相关拒答话术',
    },
    {
        'rule_type': 'EMERGENCY_RESPONSE',
        'rule_key': 'emergency_advice',
        'rule_value': {
            'message': '您描述的症状可能很紧急，请立即拨打 120 急救电话或前往最近的急诊室。',
            'action': '紧急就医',
        },
        'description': '紧急情况响应提示',
    },
    {
        'rule_type': 'SAFETY_WARNING',
        'rule_key': 'general_warning',
        'rule_value': {
            'warning': '请注意，AI 提供的信息仅供参考，不能替代专业医疗建议。',
            'level': 'medium',
        },
        'description': '通用安全警告',
    },
    {
        'rule_type': 'SIMILARITY_THRESHOLD',
        'rule_key': 'rag_similarity',
        'rule_value': {
            'threshold': 0.35,
            'metric': 'cosine',
        },
        'description': 'RAG 向量检索相似度阈值',
    },
]

# 限流规则默认值
# name 字段是代码逻辑依赖的唯一标识，必须使用英文
DEFAULT_RATE_LIMIT_RULES = [
    {
        'name': 'anonymous_global',
        'rule_type': 'GLOBAL',
        'path': None,
        'methods': [],
        'limit': 60,
        'window': 60,
        'description': '匿名用户每分钟最多 60 次请求',
    },
    {
        'name': 'authenticated_global',
        'rule_type': 'GLOBAL',
        'path': None,
        'methods': [],
        'limit': 300,
        'window': 60,
        'description': '登录用户每分钟最多 300 次请求',
    },
    {
        'name': 'anonymous_chat',
        'rule_type': 'ANONYMOUS_CHAT',
        'path': None,
        'methods': [],
        'limit': 5,
        'window': 86400,
        'description': '匿名用户每天最多 5 次聊天',
    },
    {
        'name': 'ip',
        'rule_type': 'SMS',
        'path': None,
        'methods': [],
        'limit': 10,
        'window': 3600,
        'description': 'SMS IP 每小时最多 10 次请求',
    },
    {
        'name': 'phone',
        'rule_type': 'SMS',
        'path': None,
        'methods': [],
        'limit': 5,
        'window': 3600,
        'description': 'SMS 手机号每小时最多 5 次请求',
    },
    {
        'name': 'attempts',
        'rule_type': 'SMS',
        'path': None,
        'methods': [],
        'limit': 3,
        'window': 300,
        'description': '验证码尝试每小时最多 3 次',
    },
    {
        'name': 'login',
        'rule_type': 'PATH',
        'path': '/accounts/login/',
        'methods': ['POST'],
        'limit': 10,
        'window': 300,
        'description': '登录接口 5 分钟内最多 10 次尝试',
    },
    {
        'name': 'staff_login',
        'rule_type': 'PATH',
        'path': '/accounts/staff-login/',
        'methods': ['POST'],
        'limit': 10,
        'window': 300,
        'description': '医护登录接口 5 分钟内最多 10 次尝试',
    },
    {
        'name': 'patient_login',
        'rule_type': 'PATH',
        'path': '/accounts/patient-login/',
        'methods': ['POST'],
        'limit': 10,
        'window': 300,
        'description': '患者登录接口 5 分钟内最多 10 次尝试',
    },
    {
        'name': 'phone_login',
        'rule_type': 'PATH',
        'path': '/accounts/phone-login/',
        'methods': ['POST'],
        'limit': 10,
        'window': 300,
        'description': '手机验证码登录接口 5 分钟内最多 10 次尝试',
    },
    {
        'name': 'password_reset',
        'rule_type': 'PATH',
        'path': '/accounts/password-reset/',
        'methods': ['POST'],
        'limit': 5,
        'window': 300,
        'description': '密码重置接口 5 分钟内最多 5 次尝试',
    },
    {
        'name': 'register',
        'rule_type': 'PATH',
        'path': '/accounts/register/',
        'methods': ['POST'],
        'limit': 3,
        'window': 60,
        'description': '注册接口 1 分钟内最多 3 次尝试',
    },
    {
        'name': 'chat_send',
        'rule_type': 'PATH',
        'path': '/chat/send/',
        'methods': ['POST'],
        'limit': 20,
        'window': 60,
        'description': '聊天发送 1 分钟内最多 20 次',
    },
    {
        'name': 'chat_streaming',
        'rule_type': 'PATH',
        'path': '/chat/send-streaming/',
        'methods': ['POST'],
        'limit': 20,
        'window': 60,
        'description': '聊天流式发送 1 分钟内最多 20 次',
    },
    {
        'name': 'chat_access',
        'rule_type': 'PATH',
        'path': '/chat/',
        'methods': ['GET'],
        'limit': 100,
        'window': 60,
        'description': '聊天页面访问 1 分钟内最多 100 次',
    },
]

# RAG 配置默认值
DEFAULT_RAG_CONFIGS = [
    ('embedding_dimension', 1024, 'VECTOR', '向量维度'),
    ('batch_size', 32, 'VECTOR', '批量处理大小'),
    ('chunk_size', 500, 'CHUNK', '文本切片大小'),
    ('chunk_overlap', 50, 'CHUNK', '切片重叠大小'),
    ('max_chunks', 100, 'CHUNK', '最大切片数'),
    ('top_k', 5, 'RETRIEVAL', '检索返回数量'),
    ('rerank_enabled', 0, 'RETRIEVAL', 'Rerank 全局开关'),
    ('rerank_threshold', 0.7, 'RETRIEVAL', '重排序阈值'),
    ('diversity_factor', 0.3, 'RETRIEVAL', '多样性因子'),
    ('max_articles_per_day', 100, 'OPERATION', '每日最大文章数'),
    ('min_quality_score', 0.6, 'OPERATION', '最低质量分数'),
    ('auto_publish', 0, 'OPERATION', '自动发布开关'),
]

# 系统配置默认值
DEFAULT_SYSTEM_CONFIGS = [
    # 网络配置
    ('NETWORK', 'api_timeout', 300, 'API 超时时间（秒）'),
    ('NETWORK', 'max_upload_size', 10, '最大上传大小（MB）'),
    ('NETWORK', 'cdn_enabled', 1, 'CDN 是否启用'),
    # 会话管理
    ('SESSION', 'session_timeout', 30, '会话超时时间（分钟）'),
    ('SESSION', 'max_sessions_per_user', 5, '每用户最大会话数'),
    ('SESSION', 'remember_me_days', 7, '记住我天数'),
    # AI 会话数据保留
    ('CHAT', 'session_retention_days', 90, 'AI 会话数据保留天数'),
    # 任务队列
    ('TASK', 'max_concurrent_tasks', 10, '最大并发任务数'),
    ('TASK', 'task_retry_limit', 2, '任务重试次数'),
    ('TASK', 'task_timeout_seconds', 300, '任务超时时间（秒）'),
    ('TASK', 'task_retry_delay', 60, '任务重试延迟（秒）'),
    # 品牌信息
    ('BRAND', 'brand_name', 'Health Nexus', '品牌名称'),
    ('BRAND', 'brand_logo_url', '/static/logo.png', 'Logo URL'),
    ('BRAND', 'brand_contact_email', 'admin@example.com', '联系邮箱'),
    # 短信服务
    ('SMS', 'sms_enabled', 1, '短信服务是否启用'),
]

# 品牌配置默认值
DEFAULT_BRAND_CONFIGS = [
    ('brand_name', 'Health Nexus'),
    ('brand_logo_url', '/static/logo.png'),
    ('brand_contact_email', 'admin@example.com'),
    ('brand_description', '健康宣教系统'),
]

DEFAULT_PROMPT_TEMPLATES = [
    {
        'name': '默认健康宣教模板',
        'content': '你是一个专业的健康宣教助手。你的职责是基于提供的知识库内容，为患者提供准确、易懂的健康指导。\n\n重要规则：\n1. 只回答知识库中有的内容，不要编造信息\n2. 不要做出诊断或处方建议\n3. 如果涉及紧急情况，提醒患者立即就医\n4. 所有建议仅供参考，不能替代专业医疗意见\n\n{{patient_context}}\n\n请基于以上信息回答患者的问题。',
        'is_default': True,
        'description': '系统默认的健康宣教 Prompt 模板',
    },
]
