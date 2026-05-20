from django.contrib import admin, messages
from django import forms
from django.http import JsonResponse
from django.urls import path
from django.template.response import TemplateResponse
from unfold.admin import ModelAdmin
from unfold.widgets import UnfoldAdminTextInputWidget
import requests

from apps.config.models import LLMProviderConfig, EmbeddingProviderConfig, RerankProviderConfig
from apps.config.admin.base import AuditTrackingAdminMixin


def _build_endpoint(base_url: str, path: str) -> str:
    base = base_url.rstrip('/')
    if '/models' in base or '/embeddings' in base or '/chat' in base or '/rerank' in base:
        return base
    p = path.lstrip('/') if path else ''
    return f'{base}/{p}' if p else base


class LLMProviderConfigForm(forms.ModelForm):
    class Meta:
        model = LLMProviderConfig
        fields = '__all__'

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if self.instance and self.instance.pk:
            self.fields['api_key'].required = False
            self.fields['api_key'].help_text = '留空表示保留现有 Key。如需更换，请输入新的 API Key。'
        self.fields['api_key'].widget = UnfoldAdminTextInputWidget()

    def save(self, commit=True):
        if self.instance.pk and not self.cleaned_data.get('api_key'):
            self.instance.api_key = LLMProviderConfig.objects.filter(
                pk=self.instance.pk
            ).values_list('api_key', flat=True).first() or ''
        return super().save(commit)


@admin.register(LLMProviderConfig)
class LLMProviderConfigAdmin(AuditTrackingAdminMixin, ModelAdmin):
    list_display = ['name', 'model_name', 'provider', 'is_active', 'updated_at']
    list_filter = ['provider', 'is_active']
    search_fields = ['name', 'model_name']
    list_editable = ['is_active']
    form = LLMProviderConfigForm
    fieldsets = (
        ('基本信息', {
            'fields': ('name', 'description'),
            'description': '为 LLM 配置命名，方便识别。'
        }),
        ('服务连接', {
            'fields': ('api_key', 'base_url', 'model_name', 'provider'),
            'description': 'API Key 是访问凭证，Base URL 是服务接口地址。编辑时留空表示不修改现有 Key。'
        }),
        ('响应参数', {
            'fields': ('timeout', 'max_retries', 'temperature', 'max_tokens'),
            'description': '调整请求超时、重试策略和输出参数。'
        }),
        ('状态', {
            'fields': ('is_active',),
            'description': '停用的配置不会被 LLM 轮询池使用。'
        }),
    )
    actions = ['test_connection', 'fetch_models']

    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path(
                'test-connection/',
                self.admin_site.admin_view(self._test_connection_view),
                name='config_llmproviderconfig_test_connection',
            ),
            path(
                'fetch-models/',
                self.admin_site.admin_view(self._fetch_models_view),
                name='config_llmproviderconfig_fetch_models',
            ),
        ]
        return custom_urls + urls

    def _test_connection_view(self, request):
        import json
        if request.method != 'POST':
            return JsonResponse({'success': False, 'message': '仅支持 POST 请求'})
        try:
            body = json.loads(request.body)
            base_url = body.get('base_url', '')
            api_key = body.get('api_key', '')
            model_name = body.get('model_name', '')
            if not base_url or not api_key or not model_name:
                return JsonResponse({'success': False, 'message': '缺少必要参数'})
            headers = {
                'Authorization': f'Bearer {api_key}',
                'Content-Type': 'application/json',
            }
            payload = {
                'model': model_name,
                'messages': [{'role': 'user', 'content': 'Hello, respond with "OK" only.'}],
                'max_tokens': 10,
            }
            endpoint = _build_endpoint(base_url, '/chat/completions')
            response = requests.post(endpoint, headers=headers, json=payload, timeout=30)
            if response.status_code == 200:
                return JsonResponse({
                    'success': True,
                    'message': f'连接成功！服务地址: {base_url}，模型: {model_name}'
                })
            else:
                try:
                    error_data = response.json()
                    error_msg = error_data.get('error', {}).get('message', '未知错误')
                except (json.JSONDecodeError, ValueError):
                    error_msg = response.text[:200] if response.text else '未知错误'
                return JsonResponse({
                    'success': False,
                    'message': f'连接失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': '连接超时'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except json.JSONDecodeError as e:
            return JsonResponse({'success': False, 'message': f'响应解析失败: {str(e)}'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'连接失败: {str(e)}'})

    def _fetch_models_view(self, request):
        import json
        if request.method != 'POST':
            return JsonResponse({'success': False, 'message': '仅支持 POST 请求'})
        try:
            body = json.loads(request.body)
            base_url = body.get('base_url', '')
            api_key = body.get('api_key', '')
            if not base_url or not api_key:
                return JsonResponse({'success': False, 'message': '缺少必要参数'})
            headers = {
                'Authorization': f'Bearer {api_key}',
                'Content-Type': 'application/json',
            }
            endpoint = _build_endpoint(base_url, '/models')
            response = requests.get(endpoint, headers=headers, timeout=30)
            if response.status_code == 200:
                data = response.json()
                models = sorted([item.get('id', '') for item in data.get('data', []) if item.get('id')])
                return JsonResponse({
                    'success': True,
                    'models': models,
                    'message': f'成功获取 {len(models)} 个可用模型'
                })
            else:
                try:
                    error_data = response.json()
                    error_msg = error_data.get('error', {}).get('message', '未知错误')
                except (json.JSONDecodeError, ValueError):
                    error_msg = response.text[:200] if response.text else '未知错误'
                return JsonResponse({
                    'success': False,
                    'message': f'获取失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': '请求超时'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except json.JSONDecodeError as e:
            return JsonResponse({'success': False, 'message': f'响应解析失败: {str(e)}'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'请求失败: {str(e)}'})

    def test_connection(self, request, queryset):
        if queryset.count() != 1:
            return JsonResponse({'success': False, 'message': '请选择且仅选择一条配置进行测试'})
        config = queryset.first()
        try:
            headers = {
                'Authorization': f'Bearer {config.api_key}',
                'Content-Type': 'application/json',
            }
            payload = {
                'model': config.model_name,
                'messages': [{'role': 'user', 'content': 'Hello, respond with "OK" only.'}],
                'max_tokens': 10,
            }
            endpoint = _build_endpoint(config.base_url, '/chat/completions')
            response = requests.post(endpoint, headers=headers, json=payload, timeout=config.timeout)
            if response.status_code == 200:
                return JsonResponse({
                    'success': True,
                    'message': f'连接成功！服务地址: {config.base_url}，模型: {config.model_name}'
                })
            else:
                error_data = response.json() if response.content else {}
                error_msg = error_data.get('error', {}).get('message', '未知错误')
                return JsonResponse({
                    'success': False,
                    'message': f'连接失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': f'连接超时 (超时时间: {config.timeout}秒)'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'连接失败: {str(e)}'})

    test_connection.short_description = '测试连接'

    def fetch_models(self, request, queryset):
        if queryset.count() != 1:
            return JsonResponse({'success': False, 'message': '请选择且仅选择一条配置'})
        config = queryset.first()
        try:
            headers = {
                'Authorization': f'Bearer {config.api_key}',
                'Content-Type': 'application/json',
            }
            endpoint = _build_endpoint(config.base_url, '/models')
            response = requests.get(endpoint, headers=headers, timeout=config.timeout)
            if response.status_code == 200:
                data = response.json()
                models = sorted([item.get('id', '') for item in data.get('data', []) if item.get('id')])
                return JsonResponse({
                    'success': True,
                    'models': models,
                    'message': f'成功获取 {len(models)} 个可用模型'
                })
            else:
                error_data = response.json() if response.content else {}
                error_msg = error_data.get('error', {}).get('message', '未知错误')
                return JsonResponse({
                    'success': False,
                    'message': f'获取失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': f'请求超时 (超时时间: {config.timeout}秒)'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'请求失败: {str(e)}'})

    fetch_models.short_description = '获取可用模型列表'


class EmbeddingProviderConfigForm(forms.ModelForm):
    class Meta:
        model = EmbeddingProviderConfig
        fields = '__all__'

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if self.instance and self.instance.pk:
            self.fields['api_key'].required = False
            self.fields['api_key'].help_text = '留空表示保留现有 Key。如需更换，请输入新的 API Key。'
        self.fields['api_key'].widget = UnfoldAdminTextInputWidget()

    def save(self, commit=True):
        if self.instance.pk and not self.cleaned_data.get('api_key'):
            self.instance.api_key = EmbeddingProviderConfig.objects.filter(
                pk=self.instance.pk
            ).values_list('api_key', flat=True).first() or ''
        return super().save(commit)


@admin.register(EmbeddingProviderConfig)
class EmbeddingProviderConfigAdmin(AuditTrackingAdminMixin, ModelAdmin):
    list_display = ['name', 'model_name', 'dimensions_display', 'provider', 'is_active', 'updated_at']
    list_filter = ['provider', 'is_active']
    search_fields = ['name', 'model_name']
    list_editable = ['is_active']
    form = EmbeddingProviderConfigForm
    fieldsets = (
        ('基本信息', {
            'fields': ('name', 'description'),
            'description': '为 Embedding 配置命名，方便识别。'
        }),
        ('服务连接', {
            'fields': ('api_key', 'base_url', 'model_name', 'provider'),
            'description': 'API Key 是访问凭证，Base URL 是服务接口地址。编辑时留空表示不修改现有 Key。'
        }),
        ('向量参数', {
            'fields': ('dimensions',),
            'description': 'Embedding 向量的维度。常见值: 1024 (BGE-m3), 1536 (text-embedding-3-small), 3072 (text-embedding-3-large)。与向量数据库的维度设置必须一致。'
        }),
        ('请求参数', {
            'fields': ('timeout', 'max_retries'),
            'description': '调整请求超时和重试策略。'
        }),
        ('状态', {
            'fields': ('is_active',),
            'description': '停用的配置不会被 Embedding 轮询池使用。'
        }),
    )
    actions = ['test_connection']

    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path(
                'test-connection/',
                self.admin_site.admin_view(self._test_connection_view),
                name='config_embeddingproviderconfig_test_connection',
            ),
        ]
        return custom_urls + urls

    def _test_connection_view(self, request):
        import json

        if request.method != 'POST':
            return JsonResponse({'success': False, 'message': '仅支持 POST 请求'})
        try:
            body = json.loads(request.body)
            base_url = body.get('base_url', '')
            api_key = body.get('api_key', '')
            model_name = body.get('model_name', '')
            dimensions = body.get('dimensions', 0)
            if not base_url or not api_key or not model_name:
                return JsonResponse({'success': False, 'message': '缺少必要参数'})
            headers = {
                'Authorization': f'Bearer {api_key}',
                'Content-Type': 'application/json',
            }
            payload = {
                'model': model_name,
                'input': 'Hello world',
            }
            endpoint = _build_endpoint(base_url, '/embeddings')
            response = requests.post(endpoint, headers=headers, json=payload, timeout=30)
            if response.status_code == 200:
                return JsonResponse({
                    'success': True,
                    'message': f'连接成功！服务地址: {base_url}，模型: {model_name}，维数: {dimensions}'
                })
            else:
                try:
                    error_data = response.json()
                    error_msg = error_data.get('error', {}).get('message', '未知错误')
                except (json.JSONDecodeError, ValueError):
                    error_msg = response.text[:200] if response.text else '未知错误'
                return JsonResponse({
                    'success': False,
                    'message': f'连接失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': '连接超时'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except json.JSONDecodeError as e:
            return JsonResponse({'success': False, 'message': f'响应解析失败: {str(e)}'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'连接失败: {str(e)}'})

    def test_connection(self, request, queryset):
        if queryset.count() != 1:
            return JsonResponse({'success': False, 'message': '请选择且仅选择一条配置进行测试'})
        config = queryset.first()
        try:
            headers = {
                'Authorization': f'Bearer {config.api_key}',
                'Content-Type': 'application/json',
            }
            payload = {
                'model': config.model_name,
                'input': 'Hello world',
            }
            endpoint = _build_endpoint(config.base_url, '/embeddings')
            response = requests.post(endpoint, headers=headers, json=payload, timeout=config.timeout)
            if response.status_code == 200:
                return JsonResponse({
                    'success': True,
                    'message': f'连接成功！服务地址: {config.base_url}，模型: {config.model_name}，维数: {config.dimensions}'
                })
            else:
                error_data = response.json() if response.content else {}
                error_msg = error_data.get('error', {}).get('message', '未知错误')
                return JsonResponse({
                    'success': False,
                    'message': f'连接失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': f'连接超时 (超时时间: {config.timeout}秒)'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'连接失败: {str(e)}'})

    test_connection.short_description = '测试连接'

    def dimensions_display(self, obj):
        return f"{obj.dimensions} 维"
    dimensions_display.short_description = '向量维数'


class RerankProviderConfigForm(forms.ModelForm):
    class Meta:
        model = RerankProviderConfig
        fields = '__all__'

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if self.instance and self.instance.pk:
            self.fields['api_key'].required = False
            self.fields['api_key'].help_text = '留空表示保留现有 Key。如需更换，请输入新的 API Key。'
        self.fields['api_key'].widget = UnfoldAdminTextInputWidget()

    def save(self, commit=True):
        if self.instance.pk and not self.cleaned_data.get('api_key'):
            self.instance.api_key = RerankProviderConfig.objects.filter(
                pk=self.instance.pk
            ).values_list('api_key', flat=True).first() or ''
        return super().save(commit)


@admin.register(RerankProviderConfig)
class RerankProviderConfigAdmin(AuditTrackingAdminMixin, ModelAdmin):
    list_display = ['name', 'model_name', 'top_n', 'provider', 'is_active', 'updated_at']
    list_filter = ['provider', 'is_active']
    search_fields = ['name', 'model_name']
    list_editable = ['is_active']
    form = RerankProviderConfigForm
    fieldsets = (
        ('基本信息', {
            'fields': ('name', 'description'),
            'description': '为 Rerank 配置命名，方便识别。'
        }),
        ('服务连接', {
            'fields': ('api_key', 'base_url', 'model_name', 'provider'),
            'description': 'API Key 是访问凭证，Base URL 是服务接口地址。编辑时留空表示不修改现有 Key。'
        }),
        ('Rerank 参数', {
            'fields': ('top_n',),
            'description': 'Rerank API 默认返回的文档数量。常见模型: BAAI/bge-reranker-v2-m3, jinaai/jina-reranker-v2-base-multilingual'
        }),
        ('请求参数', {
            'fields': ('timeout', 'max_retries'),
            'description': '调整请求超时和重试策略。'
        }),
        ('状态', {
            'fields': ('is_active',),
            'description': '停用的配置不会被 Rerank 轮询池使用。'
        }),
    )
    actions = ['test_connection']

    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path(
                'test-connection/',
                self.admin_site.admin_view(self._test_connection_view),
                name='config_rerankproviderconfig_test_connection',
            ),
        ]
        return custom_urls + urls

    def _test_connection_view(self, request):
        import json

        if request.method != 'POST':
            return JsonResponse({'success': False, 'message': '仅支持 POST 请求'})
        try:
            body = json.loads(request.body)
            base_url = body.get('base_url', '')
            api_key = body.get('api_key', '')
            model_name = body.get('model_name', '')
            if not base_url or not api_key or not model_name:
                return JsonResponse({'success': False, 'message': '缺少必要参数'})
            headers = {
                'Authorization': f'Bearer {api_key}',
                'Content-Type': 'application/json',
            }
            payload = {
                'model': model_name,
                'query': 'test query',
                'documents': ['test document'],
                'top_n': 1,
            }
            endpoint = _build_endpoint(base_url, '/rerank')
            response = requests.post(endpoint, headers=headers, json=payload, timeout=30)
            if response.status_code == 200:
                return JsonResponse({
                    'success': True,
                    'message': f'连接成功！服务地址: {base_url}，模型: {model_name}'
                })
            else:
                try:
                    error_data = response.json()
                    error_msg = error_data.get('error', {}).get('message', '未知错误')
                except (json.JSONDecodeError, ValueError):
                    error_msg = response.text[:200] if response.text else '未知错误'
                return JsonResponse({
                    'success': False,
                    'message': f'连接失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': '连接超时'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except json.JSONDecodeError as e:
            return JsonResponse({'success': False, 'message': f'响应解析失败: {str(e)}'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'连接失败: {str(e)}'})

    def test_connection(self, request, queryset):
        if queryset.count() != 1:
            return JsonResponse({'success': False, 'message': '请选择且仅选择一条配置进行测试'})
        config = queryset.first()
        try:
            headers = {
                'Authorization': f'Bearer {config.api_key}',
                'Content-Type': 'application/json',
            }
            payload = {
                'model': config.model_name,
                'query': 'test query',
                'documents': ['test document'],
                'top_n': 1,
            }
            endpoint = _build_endpoint(config.base_url, '/rerank')
            response = requests.post(endpoint, headers=headers, json=payload, timeout=config.timeout)
            if response.status_code == 200:
                return JsonResponse({
                    'success': True,
                    'message': f'连接成功！服务地址: {config.base_url}，模型: {config.model_name}'
                })
            else:
                error_data = response.json() if response.content else {}
                error_msg = error_data.get('error', {}).get('message', '未知错误')
                return JsonResponse({
                    'success': False,
                    'message': f'连接失败 (HTTP {response.status_code}): {error_msg}'
                })
        except requests.exceptions.Timeout:
            return JsonResponse({'success': False, 'message': f'连接超时 (超时时间: {config.timeout}秒)'})
        except requests.exceptions.ConnectionError:
            return JsonResponse({'success': False, 'message': '连接失败，无法连接到服务器'})
        except Exception as e:
            return JsonResponse({'success': False, 'message': f'连接失败: {str(e)}'})

    test_connection.short_description = '测试连接'
