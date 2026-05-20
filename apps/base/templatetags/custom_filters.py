from django import template
from django.utils.safestring import mark_safe
import markdown as md

register = template.Library()


@register.filter
def split(value, arg):
    return value.split(arg)


@register.filter
def get_item(dictionary, key):
    if dictionary is None:
        return None
    return dictionary.get(key)


@register.filter
def unique_articles(chunks):
    seen = set()
    result = []
    for chunk in chunks:
        article = chunk.article
        if article and article.id not in seen:
            seen.add(article.id)
            result.append(article)
    return result


@register.filter
def markdown(value):
    if not value:
        return ''
    return mark_safe(md.markdown(value, extensions=['fenced_code', 'tables']))
