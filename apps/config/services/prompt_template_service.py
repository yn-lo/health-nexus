import re
from apps.config.services.config_service import ConfigService
from apps.config.models import PromptTemplate


class PromptTemplateService:

    @classmethod
    def get_active_template(cls) -> PromptTemplate:
        cache_key = "prompt_template:active"
        pk = ConfigService.get(cache_key)
        if pk is not None:
            try:
                return PromptTemplate.objects.get(pk=pk, is_active=True)
            except PromptTemplate.DoesNotExist:
                pass

        template = PromptTemplate.objects.filter(is_default=True, is_active=True).first()
        if template:
            ConfigService.set(cache_key, template.pk)
            return template

        return None

    @classmethod
    def get_active_content(cls) -> str:
        template = cls.get_active_template()
        if template:
            return template.content
        return ""

    @classmethod
    def render(cls, variables: dict = None) -> str:
        content = cls.get_active_content()
        if not content:
            return ""

        variables = variables or {}
        for key, value in variables.items():
            content = content.replace(f"{{{{{key}}}}}", str(value))

        content = re.sub(r'\{\{\w+\}\}', '', content)
        content = re.sub(r'[，、；]\s*$', '', content, flags=re.MULTILINE)
        content = re.sub(r'^\s*[，、；：]\s*$', '', content, flags=re.MULTILINE)
        return re.sub(r'\n{3,}', '\n\n', content).strip()

    @classmethod
    def activate_template(cls, template_id) -> PromptTemplate:
        template = PromptTemplate.objects.get(pk=template_id)
        template.is_default = True
        template.save()
        return template
