import json

from django.db import models
from django.utils.translation import gettext_lazy as _
from django.conf import settings
from django.core.exceptions import ValidationError
from django.contrib.postgres.search import SearchVectorField
from django.contrib.postgres.indexes import GinIndex
from pgvector.django import VectorField
from django_quill.fields import QuillField, FieldQuill, QuillDescriptor
from django_quill.quill import Quill, QuillParseError
from apps.base.models import TimeStampedModel, Department
from apps.auth.models import UserProfile


def validate_image_file(value, field_name=_('图片')):
    """Validate image file size"""
    if value:
        max_size = 10 * 1024 * 1024
        if value.size > max_size:
            raise ValidationError(f'{field_name}大小不能超过 10MB')


def validate_cover_image(value):
    """Validate cover image file"""
    validate_image_file(value, _('封面图'))


class ForgivingFieldQuill(FieldQuill):
    """容错版 FieldQuill - 格式错误时不抛出 QuillParseError"""

    @property
    def plain(self):
        try:
            return super().plain
        except QuillParseError:
            return ''

    @property
    def html(self):
        try:
            return super().html
        except QuillParseError:
            return ''

    @property
    def delta(self):
        try:
            return super().delta
        except QuillParseError:
            return {}


class ForgivingQuillDescriptor(QuillDescriptor):
    """容错版 QuillDescriptor - 使用 ForgivingFieldQuill"""

    def __get__(self, instance, cls=None):
        if instance is None:
            return self

        if self.field.name in instance.__dict__:
            quill = instance.__dict__[self.field.name]
        else:
            instance.refresh_from_db(fields=[self.field.name])
            quill = getattr(instance, self.field.name)

        if isinstance(quill, str) or quill is None:
            attr = ForgivingFieldQuill(instance, self.field, quill)
            instance.__dict__[self.field.name] = attr

        elif isinstance(quill, Quill) and not isinstance(quill, ForgivingFieldQuill):
            quill_copy = ForgivingFieldQuill(instance, self.field, quill.json_string)
            quill_copy.quill = quill
            quill_copy._committed = False
            instance.__dict__[self.field.name] = quill_copy

        elif isinstance(quill, ForgivingFieldQuill) and not hasattr(quill, "field"):
            quill.instance = instance
            quill.field = self.field

        elif isinstance(quill, ForgivingFieldQuill) and instance is not quill.instance:
            quill.instance = instance

        return instance.__dict__[self.field.name]

    def __set__(self, instance, value):
        instance.__dict__[self.field.name] = value


class ForgivingQuillField(QuillField):
    """容错版 QuillField - 使用 ForgivingFieldQuill"""
    attr_class = ForgivingFieldQuill
    descriptor_class = ForgivingQuillDescriptor

    def to_python(self, value):
        """重写 to_python，格式错误时直接返回原始字符串"""
        if isinstance(value, Quill):
            return value
        if isinstance(value, FieldQuill):
            try:
                return value.quill
            except QuillParseError:
                return value.json_string
        if value is None or isinstance(value, str):
            return value
        try:
            return Quill(value)
        except QuillParseError:
            return value


class Article(TimeStampedModel):
    """
    文章容器 - 用于展示
    """
    class Status(models.TextChoices):
        DRAFT = 'DRAFT', _('草稿')
        PENDING = 'PENDING', _('待审核')
        PUBLISHED = 'PUBLISHED', _('已发布')
        ARCHIVED = 'ARCHIVED', _('已下线')

    title = models.CharField(_("标题"), max_length=200)
    
    # v1.2 新增字段
    summary = models.CharField(_("摘要"), max_length=300, blank=True, help_text=_("用于列表页展示，留空自动截取"))
    cover_image = models.ImageField(
        _("封面图"),
        upload_to='articles/covers/',
        blank=True,
        validators=[validate_cover_image]
    )
    view_count = models.PositiveIntegerField(_("阅读量"), default=0)

    content = ForgivingQuillField(_("HTML内容"), help_text=_("用于前端完整展示"))
    
    author = models.ForeignKey(UserProfile, on_delete=models.SET_NULL, null=True, verbose_name=_("作者"))
    department = models.ForeignKey(Department, on_delete=models.CASCADE, verbose_name=_("所属科室"))
    status = models.CharField(_("状态"), max_length=20, choices=Status.choices, default=Status.DRAFT)
    
    # 来源标记
    source_type = models.CharField(_("来源"), max_length=20, default='MANUAL', choices=[('MANUAL', '人工撰写'), ('AI_IMPORT', 'AI导入')])
    
    version = models.PositiveIntegerField(_("版本号"), default=1, help_text=_("每次修改已发布文章版本号递增"))
    is_deleted = models.BooleanField(_("软删除"), default=False)

    review_due_date = models.DateField(_("复审日期"), null=True, blank=True)
    review_overdue = models.BooleanField(_("复审已到期"), default=False, help_text=_("到期后保持发布但标记待复核"))
    allow_reference = models.BooleanField(
        _("允许引用"),
        default=False,
        help_text=_("是否允许子科室引用此公共文章")
    )

    class Meta:
        verbose_name = _("宣教文章")
        verbose_name_plural = _("宣教文章")
        ordering = ['-created_at']

    def __str__(self):
        return self.title

    def save(self, *args, **kwargs):
        if not self.pk:
            self.version = 1

        if not self.summary and self.content:
            try:
                content_plain = getattr(self.content, 'plain', None)
                if content_plain is None:
                    content_data = getattr(self.content, 'data', None)
                    if isinstance(content_data, dict):
                        content_plain = content_data.get('plain', '') or ''
                    else:
                        content_plain = str(content_data) if content_data else ''
                self.summary = content_plain[:100] + '...' if len(content_plain) > 100 else content_plain
            except Exception:
                pass
        super().save(*args, **kwargs)


class ArticleChunk(models.Model):
    """
    向量切片 - 用于 RAG 检索
    """
    article = models.ForeignKey(Article, on_delete=models.CASCADE, related_name='chunks', verbose_name=_("源文章"))
    content_text = models.TextField(_("纯文本切片"), help_text=_("清洗了HTML标签的文本，用于喂给大模型"))

    embedding = VectorField(dimensions=settings.VECTOR_DIMENSIONS, verbose_name=_("向量数据"))

    chunk_index = models.IntegerField(_("切片序号"), default=0)

    department = models.ForeignKey(Department, on_delete=models.CASCADE, verbose_name=_("冗余科室ID"))

    version = models.PositiveIntegerField(_("版本号"), default=1, help_text=_("所属文章版本号"))
    is_active = models.BooleanField(_("生效中"), default=True, help_text=_("当前生效的切片"))

    content_hash = models.CharField(_("内容哈希"), max_length=32, blank=True, default='', help_text=_("用于检测内容是否变化"))

    search_vector = SearchVectorField(_("全文搜索向量"), null=True, blank=True, help_text=_("PostgreSQL 全文搜索向量，用于 BM25 检索"))

    class Meta:
        verbose_name = _("知识切片")
        verbose_name_plural = _("知识切片")
        indexes = [
            models.Index(fields=['department'], name='idx_chunk_dept'),
            models.Index(fields=['article', 'chunk_index'], name='idx_article_chunk'),
            GinIndex(fields=['search_vector'], name='idx_chunk_search_vector'),
        ]

    def __str__(self):
        return f"{self.article.title} - #{self.chunk_index}"


class ArticleAuditLog(models.Model):
    """
    文章状态变更审计日志
    """
    article = models.ForeignKey(Article, on_delete=models.CASCADE, related_name='audit_logs', verbose_name=_("关联文章"))
    old_status = models.CharField(_("变更前状态"), max_length=20, null=True, blank=True, choices=Article.Status.choices)
    new_status = models.CharField(_("变更后状态"), max_length=20, choices=Article.Status.choices)
    changed_by = models.ForeignKey(UserProfile, on_delete=models.SET_NULL, null=True, blank=True, verbose_name=_("操作用户"))
    reason = models.TextField(_("变更原因"), blank=True, default='')
    change_summary = models.TextField(_("变更内容摘要"), blank=True, default='', help_text=_("编辑时记录变更内容摘要"))
    created_at = models.DateTimeField(_("变更时间"), auto_now_add=True)

    class Meta:
        verbose_name = "文章审计日志"
        verbose_name_plural = "文章审计日志"
        ordering = ['-created_at']

    def __str__(self):
        return f"{self.article.title} - {self.old_status} → {self.new_status}"
