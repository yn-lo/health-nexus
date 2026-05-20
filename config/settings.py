"""
Django settings for health_nexus project.
"""

import os
import sys
from pathlib import Path
from dotenv import load_dotenv

# Build paths inside the project like this: BASE_DIR / 'subdir'.
BASE_DIR = Path(__file__).resolve().parent.parent

# Add apps directory to sys.path
sys.path.append(str(BASE_DIR / 'apps'))

# Load .env file (override=False to respect existing env vars like DEBUG=True in tests)
load_dotenv(BASE_DIR / '.env', override=False)


# Quick-start development settings - unsuitable for production
# See https://docs.djangoproject.com/en/5.0/howto/deployment/checklist/

# SECURITY WARNING: keep the secret key used in production secret!
SECRET_KEY = os.environ.get('SECRET_KEY', 'django-insecure-change-me-in-production')

# SECURITY WARNING: don't run with debug turned on in production!
_DEBUG_ENV = os.environ.get('DEBUG', 'True').lower() == 'true'
# Force DEBUG=True when running tests (pytest-django loads settings before PYTEST_CURRENT_TEST is set)
_IS_PYTEST = any("pytest" in arg for arg in sys.argv) or os.environ.get('PYTEST_CURRENT_TEST')
DEBUG = True if _IS_PYTEST else _DEBUG_ENV

ALLOWED_HOSTS = os.environ.get('ALLOWED_HOSTS', '').split(',') if os.environ.get('ALLOWED_HOSTS') else ['localhost', '127.0.0.1']


# Application definition

INSTALLED_APPS = [
    # Unfold Admin - 必须在django.contrib.admin之前
    "unfold",
    
    'django.contrib.admin',
    'django.contrib.auth',
    'django.contrib.contenttypes',
    'django.contrib.sessions',
    'django.contrib.messages',
    'django.contrib.staticfiles',
    'django.contrib.postgres',
    
    # Third party
    'pgvector',
    'django_quill',
    'django_q',
    'encrypted_model_fields',
    'ninja',

    # Local apps
    'apps.base',
    'apps.auth',
    'apps.wiki',
    'apps.chat',
    'apps.care',
    'apps.config',
    'apps.stats',
]

MIDDLEWARE = [
    'django.middleware.security.SecurityMiddleware',
    'whitenoise.middleware.WhiteNoiseMiddleware',
    'django.contrib.sessions.middleware.SessionMiddleware',
    'django.middleware.common.CommonMiddleware',
    'django.middleware.csrf.CsrfViewMiddleware',
    'django.contrib.auth.middleware.AuthenticationMiddleware',
    'django.contrib.messages.middleware.MessageMiddleware',
    'apps.auth.middleware.SingleSessionMiddleware',
    'apps.auth.middleware.RolePermissionMiddleware',
    'apps.auth.middleware.RoleBasedRouteMiddleware',
    'apps.auth.middleware.TermsAgreementMiddleware',
    'apps.base.middleware.observability.ObservabilityMiddleware',
    'apps.auth.rate_limit.middleware.RateLimitMiddleware',
    'django.middleware.clickjacking.XFrameOptionsMiddleware',
    'apps.base.middleware.security.SecurityHeadersMiddleware',
]

# 反向代理配置
SECURE_PROXY_SSL_HEADER = ('HTTP_X_FORWARDED_PROTO', 'https')
USE_X_FORWARDED_HOST = True

# CSRF 配置
CSRF_TRUSTED_ORIGINS = os.environ.get(
    'CSRF_TRUSTED_ORIGINS',
    'https://nedu.sg.roynz.cn,http://nedu.sg.roynz.cn'
).split(',')
CSRF_COOKIE_SECURE = os.environ.get('CSRF_COOKIE_SECURE', 'True').lower() == 'true'
CSRF_COOKIE_HTTPONLY = True
CSRF_COOKIE_SAMESITE = 'Lax'

# Session Security
SESSION_COOKIE_SECURE = os.environ.get('SESSION_COOKIE_SECURE', 'True').lower() == 'true'
SESSION_COOKIE_HTTPONLY = True
SESSION_COOKIE_SAMESITE = 'Lax'
SESSION_COOKIE_AGE = 3600
SESSION_EXPIRE_AT_BROWSER_CLOSE = True

# 静态文件访问配置
X_FRAME_OPTIONS = 'SAMEORIGIN'

# Security Headers
SECURE_CONTENT_TYPE_NOSNIFF = True
SECURE_SSL_REDIRECT = os.environ.get('SECURE_SSL_REDIRECT', 'False').lower() == 'true'

# CORS Configuration
CORS_ALLOWED_ORIGINS = os.environ.get(
    'CORS_ALLOWED_ORIGINS',
    'https://nedu.sg.roynz.cn,http://nedu.sg.roynz.cn'
).split(',')

# File Upload Size Limits
DATA_UPLOAD_MAX_MEMORY_SIZE = 10 * 1024 * 1024  # 10MB
FILE_UPLOAD_MAX_MEMORY_SIZE = 10 * 1024 * 1024  # 10MB

ROOT_URLCONF = 'config.urls'

TEMPLATES = [
    {
        'BACKEND': 'django.template.backends.django.DjangoTemplates',
        'DIRS': [BASE_DIR / 'templates'],
        'APP_DIRS': True,
        'OPTIONS': {
            'context_processors': [
                'django.template.context_processors.debug',
                'django.template.context_processors.request',
                'django.contrib.auth.context_processors.auth',
                'django.contrib.messages.context_processors.messages',
                'apps.base.brand_config.brand_context',
            ],
        },
    },
]

WSGI_APPLICATION = 'config.wsgi.application'


# Database
# https://docs.djangoproject.com/en/5.0/ref/settings/#databases

# 检测是否在Docker环境中运行
def is_running_in_docker():
    """检查当前是否在Docker容器中运行"""
    try:
        with open('/proc/1/cgroup', 'r') as f:
            return 'docker' in f.read() or 'kubepods' in f.read()
    except (IOError, OSError):
        return False

# 根据环境设置数据库主机
if is_running_in_docker():
    # 在Docker环境中，使用服务名作为主机
    DEFAULT_DB_HOST = 'db'
else:
    # 在本地开发环境中，使用localhost
    DEFAULT_DB_HOST = 'localhost'

def _parse_int_env(key, default):
    """安全解析整型环境变量，非数字时返回默认值"""
    value = os.environ.get(key, str(default))
    try:
        return int(value)
    except (ValueError, TypeError):
        return default

SECURE_HSTS_SECONDS = _parse_int_env('SECURE_HSTS_SECONDS', 0)
SECURE_HSTS_INCLUDE_SUBDOMAINS = True

DATABASES = {
    'default': {
        'ENGINE': 'django.db.backends.postgresql',
        'NAME': os.environ.get('DB_NAME', 'health_nexus'),
        'USER': os.environ.get('DB_USER', 'postgres'),
        'PASSWORD': os.environ.get('DB_PASSWORD', 'postgres'),
        'HOST': os.environ.get('DB_HOST', DEFAULT_DB_HOST),
        'PORT': os.environ.get('DB_PORT', '5432'),
        'CONN_MAX_AGE': _parse_int_env('DB_CONN_MAX_AGE', 60),
        'OPTIONS': {
            'connect_timeout': 10,
        },
        'TEST': {
            'NAME': 'test_health_nexus',
        },
    }
}

# 使用SQLite进行测试以提高稳定性和性能（可通过 USE_TEST_DB=postgresql 覆盖）
import sys
if ('test' in sys.argv or 'pytest' in sys.modules) and os.environ.get('USE_TEST_DB', 'sqlite') == 'sqlite':
    DATABASES['default'] = {
        'ENGINE': 'django.db.backends.sqlite3',
        'NAME': ':memory:',
    }


# Password validation
# https://docs.djangoproject.com/en/5.0/ref/settings/#auth-password-validators

AUTH_PASSWORD_VALIDATORS = [
    {
        'NAME': 'django.contrib.auth.password_validation.UserAttributeSimilarityValidator',
    },
    {
        'NAME': 'django.contrib.auth.password_validation.MinimumLengthValidator',
    },
]

if not DEBUG:
    AUTH_PASSWORD_VALIDATORS += [
        {
            'NAME': 'django.contrib.auth.password_validation.CommonPasswordValidator',
        },
        {
            'NAME': 'django.contrib.auth.password_validation.NumericPasswordValidator',
        },
    ]


# Internationalization
# https://docs.djangoproject.com/en/5.0/topics/i18n/

LANGUAGE_CODE = 'zh-hans'

TIME_ZONE = 'Asia/Shanghai'

USE_I18N = True

USE_TZ = True


# Static files (CSS, JavaScript, Images)
# https://docs.djangoproject.com/en/5.0/howto/static-files/

STATIC_URL = '/static/'
STATIC_ROOT = BASE_DIR / 'staticfiles'

# 静态文件目录配置
STATICFILES_DIRS = [
    BASE_DIR / 'static',
]

# 静态文件配置 - 解决反向代理问题
STATICFILES_STORAGE = 'whitenoise.storage.CompressedStaticFilesStorage'
STATICFILES_FINDERS = [
    'django.contrib.staticfiles.finders.FileSystemFinder',
    'django.contrib.staticfiles.finders.AppDirectoriesFinder',
]

# Media files
MEDIA_URL = '/media/'
MEDIA_ROOT = BASE_DIR / 'mediafiles'

# Default primary key field type
# https://docs.djangoproject.com/en/5.0/ref/settings/#default-auto-field

DEFAULT_AUTO_FIELD = 'django.db.models.BigAutoField'

# Custom User Model
AUTH_USER_MODEL = 'auth_custom.UserProfile'

# 认证配置
LOGIN_REDIRECT_URL = '/chat/'
LOGIN_URL = 'auth:login'
LOGOUT_REDIRECT_URL = '/chat/'
ADMIN_LOGIN_REDIRECT_URL = '/admin/'

# 限流配置
RATE_LIMIT_ENABLED = os.environ.get('RATE_LIMIT_ENABLED', 'True').lower() == 'true'

# Quill Editor Settings (optional, for customization)
QUILL_CONFIGS = {
    'default': {
        'theme': 'snow',
        'modules': {
            'syntax': True,
            'toolbar': [
                [
                    {'font': []},
                    {'header': []},
                    {'align': []},
                    'bold', 'italic', 'underline', 'strike', 'blockquote',
                    {'color': []}, {'background': []},
                ],
                ['link', 'image', 'video'],
                ['clean'],
            ]
        }
    }
}

# Django Q
Q_CLUSTER = {
    'name': 'health_nexus',
    'workers': _parse_int_env('Q_WORKERS', 8),
    'recycle': 500,
    'timeout': 60,
    'compress': True,
    'save_limit': 250,
    'queue_limit': 500,
    'cpu_affinity': 1,
    'label': 'Django Q',
    'orm': 'default',
    'redis': os.environ.get('Q_REDIS', 'redis://redis:6379/0'),
}

# Cache Configuration
CACHES = {
    'default': {
        'BACKEND': 'django.core.cache.backends.redis.RedisCache',
        'LOCATION': os.environ.get('CACHE_REDIS', 'redis://redis:6379/1'),
        'TIMEOUT': 3600,
    }
}

if _IS_PYTEST or 'test' in sys.argv:
    CACHES = {
        'default': {
            'BACKEND': 'django.core.cache.backends.locmem.LocMemCache',
            'LOCATION': 'health-nexus-test',
        }
    }

# Cache key prefix for config
CONFIG_CACHE_PREFIX = 'config'

# Encryption Key (For demo purposes, using a fixed key. In production, use environment variable)
# Generate one using: import cryptography.fernet; cryptography.fernet.Fernet.generate_key()
FIELD_ENCRYPTION_KEY = os.environ.get('FIELD_ENCRYPTION_KEY', 'hz4jWc8wsgr4_Vp3EaPeY5rY26TTXntqtBiQyacbZqg=')

# 业务配置已迁移到数据库管理
# 通过 Django Admin → 系统配置 模块管理
# 默认配置通过 Data Migration 导入
# 读取优先级：Redis 缓存 → 数据库

# 数据库结构级配置（不可动态修改，需要迁移时固定）
VECTOR_DIMENSIONS = _parse_int_env('VECTOR_DIMENSIONS', 1024)  # 向量维度，与 Embedding 模型匹配

# 文本切片配置
CHUNK_SIZE = _parse_int_env('CHUNK_SIZE', 500)  # 文本切片大小
CHUNK_OVERLAP = _parse_int_env('CHUNK_OVERLAP', 50)  # 文本切片重叠

LOGGING = {
    'version': 1,
    'disable_existing_loggers': False,
    'formatters': {
        'verbose': {
            'format': '{asctime} | {levelname} | {name} | {message}',
            'style': '{',
            'datefmt': '%Y-%m-%d %H:%M:%S',
        },
        'audit': {
            'format': '{asctime} | AUDIT | {message}',
            'style': '{',
            'datefmt': '%Y-%m-%d %H:%M:%S',
        },
    },
    'handlers': {
        'console': {
            'level': 'INFO',
            'class': 'logging.StreamHandler',
            'formatter': 'verbose',
        },
        'audit': {
            'level': 'INFO',
            'class': 'logging.StreamHandler',
            'formatter': 'audit',
        },
        'slow_query': {
            'level': 'WARNING',
            'class': 'logging.StreamHandler',
            'formatter': 'verbose',
        },
    },
    'loggers': {
        'health_nexus.observability': {
            'handlers': ['console'],
            'level': 'INFO',
            'propagate': False,
        },
        'health_nexus.slow_queries': {
            'handlers': ['slow_query'],
            'level': 'WARNING',
            'propagate': False,
        },
        'audit': {
            'handlers': ['audit'],
            'level': 'INFO',
            'propagate': False,
        },
    },
}

from django.urls import reverse_lazy
from django.utils.translation import gettext_lazy as _

UNFOLD = {
    "SITE_TITLE": "Health Nexus",
    "SITE_HEADER": "Health Nexus 健康宣教系统",
    "SITE_URL": "/",
    "SIDEBAR": {
        "show_search": True,
        "show_all_applications": True,
        "navigation": [
            {
                "title": _("核心业务"),
                "separator": True,
                "items": [
                    {
                        "title": _("科室管理"),
                        "icon": "domain",
                        "link": "/admin/base/department/",
                    },
                    {
                        "title": _("宣教文章"),
                        "icon": "article",
                        "link": "/admin/wiki/article/",
                    },
                    {
                        "title": _("知识切片"),
                        "icon": "segment",
                        "link": "/admin/wiki/articlechunk/",
                    },
                ],
            },
            {
                "title": _("用户管理"),
                "separator": True,
                "items": [
                    {
                        "title": _("用户账户"),
                        "icon": "people",
                        "link": "/admin/auth_custom/userprofile/",
                    },
                    {
                        "title": _("批量导入用户"),
                        "icon": "upload_file",
                        "link": "/admin/auth_custom/userprofile/bulk_import/",
                    },
                    {
                        "title": _("患者档案"),
                        "icon": "medical_services",
                        "link": "/admin/care/patientprofile/",
                    },
                ],
            },
            {
                "title": _("AI 对话"),
                "separator": True,
                "items": [
                    {
                        "title": _("会话记录"),
                        "icon": "chat",
                        "link": "/admin/chat/conversation/",
                    },
                    {
                        "title": _("消息明细"),
                        "icon": "message",
                        "link": "/admin/chat/message/",
                    },
                ],
            },
            {
                "title": _("系统监控"),
                "separator": True,
                "items": [
                    {
                        "title": _("成功任务"),
                        "icon": "check_circle",
                        "link": "/admin/django_q/success/",
                    },
                    {
                        "title": _("失败任务"),
                        "icon": "error",
                        "link": "/admin/django_q/failure/",
                    },
                    {
                        "title": _("计划任务"),
                        "icon": "schedule",
                        "link": "/admin/django_q/schedule/",
                    },
                ],
            },
            {
                "title": _("系统配置"),
                "separator": True,
                "items": [
                    {
                        "title": _("配置总览"),
                        "icon": "dashboard",
                        "link": "/admin/config-dashboard/",
                    },
                    {
                        "title": _("LLM配置"),
                        "icon": "settings",
                        "link": "/admin/config/llmproviderconfig/",
                    },
                    {
                        "title": _("Embedding配置"),
                        "icon": "settings",
                        "link": "/admin/config/embeddingproviderconfig/",
                    },
                    {
                        "title": _("Rerank配置"),
                        "icon": "settings",
                        "link": "/admin/config/rerankproviderconfig/",
                    },
                    {
                        "title": _("敏感词"),
                        "icon": "settings",
                        "link": "/admin/config/sensitiveword/",
                    },
                    {
                        "title": _("安全规则"),
                        "icon": "settings",
                        "link": "/admin/config/safetyrule/",
                    },
                    {
                        "title": _("限流规则"),
                        "icon": "settings",
                        "link": "/admin/config/ratelimitrule/",
                    },
                    {
                        "title": _("RAG配置"),
                        "icon": "settings",
                        "link": "/admin/config/ragconfig/",
                    },
                    {
                        "title": _("系统配置"),
                        "icon": "settings",
                        "link": "/admin/config/systemconfig/",
                    },
                    {
                        "title": _("品牌配置"),
                        "icon": "settings",
                        "link": "/admin/config/brandconfig/",
                    },
                ],
            },
        ],
    },
}

SHOW_TEST_ACCOUNTS_PAGE = True

TEST_ACCOUNTS = {
    'patient': {
        'username': 'test_patient',
        'password': 'Test123456',
        'role': '患者',
        'login_url': '/accounts/login/',
        'home_url': '/chat/',
    },
    'doctor': {
        'username': 'test_doctor',
        'password': 'Test123456',
        'role': '医生',
        'login_url': '/accounts/staff-login/',
        'home_url': '/staff/dashboard/',
    },
    'nurse': {
        'username': 'test_nurse',
        'password': 'Test123456',
        'role': '护士',
        'login_url': '/accounts/staff-login/',
    },
    'admin': {
        'username': 'admin',
        'password': 'admin',
        'role': '管理员',
        'login_url': '/admin/login/',
    },
}

CHAT_ANONYMOUS_MIGRATION_ENABLED = os.environ.get(
    'CHAT_ANONYMOUS_MIGRATION_ENABLED', 'True'
).lower() == 'true'
