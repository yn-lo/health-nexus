import json
from django_quill.widgets import QuillWidget
from django_quill.forms import QuillFormField
import django_quill.config

LOCAL_QUILL_JS = [
    "vendor/highlight/highlight.min.js",
    "vendor/quill/quill.min.js",
    "vendor/quill-image-compress/quill.imageCompressor.min.js",
    "vendor/quill-resize-module/quill-resize-module.min.js",
    "django_quill/django_quill.js",
]

LOCAL_QUILL_CSS = {
    "all": [
        "vendor/quill/quill.snow.css",
        "vendor/highlight/darcula.min.css",
        "vendor/quill-resize-module/resize.min.css",
        "django_quill/django_quill.css",
    ]
}

# Monkey-patch the global config used by QuillWidget.Media
django_quill.config.MEDIA_JS = LOCAL_QUILL_JS
django_quill.config.MEDIA_CSS = LOCAL_QUILL_CSS

# Patch QuillWidget Media class (QuillFormField gets media from its widget)
QuillWidget.Media.js = LOCAL_QUILL_JS
QuillWidget.Media.css = LOCAL_QUILL_CSS


class LocalQuillWidget(QuillWidget):
    """使用本地静态文件的 Quill 编辑器 widget"""

    class Media:
        js = LOCAL_QUILL_JS
        css = LOCAL_QUILL_CSS


class LocalQuillFormField(QuillFormField):
    """使用本地静态文件的 Quill 表单字段

    兼容纯 Delta 格式（{"ops":[...]}）和 django_quill 包装格式
    （{"delta":{...},"html":"..."}），在 clean 方法中统一转换为
    django_quill 期望的格式。
    """

    def __init__(self, *args, **kwargs):
        kwargs.update({
            "widget": LocalQuillWidget(),
        })
        super(QuillFormField, self).__init__(*args, **kwargs)

    def clean(self, value):
        value = super().clean(value)
        if not value:
            return value
        try:
            data = json.loads(value)
        except (json.JSONDecodeError, TypeError):
            return value

        # 已经是正确格式（有 delta 和 html 键）
        if isinstance(data, dict) and "delta" in data and "html" in data:
            return value

        # 纯 Delta 格式（{"ops":[...]}）-> 包装格式
        if isinstance(data, dict) and "ops" in data:
            wrapped = {
                "delta": data,
                "html": "",
            }
            return json.dumps(wrapped, ensure_ascii=False)

        # delta 是字符串的错误格式（{"delta":"{\"ops\":[...]}"}）-> 修复
        if isinstance(data, dict) and isinstance(data.get("delta"), str):
            try:
                data["delta"] = json.loads(data["delta"])
            except (json.JSONDecodeError, TypeError):
                pass
            data.setdefault("html", "")
            return json.dumps(data, ensure_ascii=False)

        return value
