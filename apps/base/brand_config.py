"""
Health Nexus 品牌配置 - 从数据库动态加载，回退到硬编码默认值
"""
from apps.config.services.system_service import SystemConfigService


BRAND_CONFIG_DEFAULTS = {
    'name': 'Health Nexus',
    'full_name': 'Health Nexus 智能健康宣教系统',
    'tagline': 'AI 智能健康助手',
    'description': '基于人工智能的健康宣教知识库系统',
    'version': '1.0.0',
    'build': 'production',
    'owner': {
        'name': 'YNLO',
        'company': 'YnLo Solo team',
        'website': 'https://www.royzn.cn',
        'email': 'czwziy@qq.com',
        'phone': '400-xxx-xxxx',
    },
    'support': {
        'name': '技术支持',
        'team': 'YNLO',
        'response_time': '24小时内也许不响应',
        'hotline': '400-xxx-xxxx',
    },
    'social': {
        'wechat': 'czwziy',
        'weibo': '@czwziy',
        'github': 'https://github.com/czwziy',
    },
    'legal': {
        'copyright': '© 2024 YNLO. 保留所有权利.',
        'license': 'MIT License',
        'privacy_policy': '/privacy/',
        'terms_of_service': '/terms/',
    },
    'colors': {
        'primary': '#10b981',
        'secondary': '#14b8a6',
        'accent': '#06b6d4',
        'success': '#10b981',
        'warning': '#f59e0b',
        'error': '#ef4444',
        'gradient': 'linear-gradient(135deg, #10b981 0%, #14b8a6 100%)',
    },
    'slogans': [
        '智能健康，触手可及',
        'AI赋能，健康守护',
        '科技让健康更简单',
        '您的智能健康助手',
    ],
    'features': [
        'AI智能问答',
        '专业健康知识',
        '个性化推荐',
        '多科室覆盖',
        '实时更新',
        '安全可靠',
    ],
}


def _get_brand_config():
    """从三级架构获取品牌配置：缓存 → 数据库 → 默认值"""
    db_config = SystemConfigService.get_brand_config()
    config = BRAND_CONFIG_DEFAULTS.copy()
    for key in ('owner', 'support', 'social', 'legal', 'colors'):
        config[key] = config[key].copy()
    config.update({
        'name': db_config.get('brand_name', config['name']),
        'tagline': db_config.get('brand_tagline', config['tagline']),
        'owner': {
            **config['owner'],
            'name': db_config.get('brand_owner_name', config['owner']['name']),
            'website': db_config.get('brand_owner_website', config['owner']['website']),
        },
        'support': {
            **config['support'],
            'hotline': db_config.get('brand_support_hotline', config['support']['hotline']),
        },
        'legal': {
            **config['legal'],
            'copyright': db_config.get('brand_copyright', config['legal']['copyright']),
        },
        'colors': {
            **config['colors'],
            'primary': db_config.get('brand_primary_color', config['colors']['primary']),
        },
    })
    return config


def brand_context(request):
    """Django上下文处理器，将品牌配置注入到非Admin模板中"""
    if request.path.startswith('/admin/'):
        return {}
    return {'brand': _get_brand_config()}


def get_brand_info():
    """获取完整的品牌配置"""
    return _get_brand_config()


def get_brand_color(color_name):
    """获取品牌颜色"""
    return _get_brand_config()['colors'].get(color_name, '#000000')


def get_owner_info():
    """获取所有者信息"""
    return _get_brand_config()['owner']


def get_support_info():
    """获取技术支持信息"""
    return _get_brand_config()['support']


def get_copyright_text():
    """获取版权文本"""
    return _get_brand_config()['legal']['copyright']


def get_seo_config():
    """获取SEO配置"""
    config = _get_brand_config()
    return {
        'default_title': f"{config['name']} - {config['tagline']}",
        'default_description': config['description'],
        'default_keywords': '健康,AI,智能问答,医疗,宣教,知识库',
        'author': config['owner']['name'],
        'robots': 'index, follow',
        'og_site_name': config['name'],
        'og_type': 'website',
        'og_locale': 'zh_CN',
    }
