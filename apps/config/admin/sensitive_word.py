from django.contrib import admin, messages
from django.http import HttpResponse
from django.urls import reverse
from django.shortcuts import redirect
from unfold.admin import ModelAdmin
from apps.config.models import SensitiveWord
import csv


@admin.register(SensitiveWord)
class SensitiveWordAdmin(ModelAdmin):
    list_display = ['word', 'category_display', 'is_active', 'created_at']
    list_filter = ['category', 'is_active']
    search_fields = ['word']
    list_editable = ['is_active']
    ordering = ['category', 'word']
    actions = ['export_sensitive_words']
    radio_fields = {'category': admin.HORIZONTAL}

    def category_display(self, obj):
        return obj.get_category_display()
    category_display.short_description = '分类'

    def changelist_view(self, request, extra_context=None):
        if request.method == 'POST' and 'file' in request.FILES:
            return self._handle_import(request)
        context = extra_context or {}
        context['import_help'] = True
        return super().changelist_view(request, extra_context=context)

    def _handle_import(self, request):
        csv_file = request.FILES.get('file')
        if not csv_file:
            self.message_user(request, '请上传 CSV 文件', messages.ERROR)
            return self._redirect_to_changelist(request)

        decoded_file = csv_file.read().decode('utf-8').splitlines()
        reader = csv.DictReader(decoded_file)

        created_count = 0
        skipped_count = 0

        for row in reader:
            word = row.get('word', '').strip()
            category = row.get('category', '').strip()

            if not word or not category:
                continue

            if SensitiveWord.objects.filter(word=word).exists():
                skipped_count += 1
                continue

            try:
                SensitiveWord.objects.create(word=word, category=category)
                created_count += 1
            except Exception:
                skipped_count += 1

        msg = f'成功导入 {created_count} 条，跳过 {skipped_count} 条重复记录'
        self.message_user(request, msg, messages.SUCCESS)
        return self._redirect_to_changelist(request)

    def _redirect_to_changelist(self, request):
        return redirect(reverse('admin:config_sensitiveword_changelist'))

    def export_sensitive_words(self, request, queryset):
        response = HttpResponse(content_type='text/csv')
        response['Content-Disposition'] = 'attachment; filename="sensitive_words.csv"'

        writer = csv.writer(response)
        writer.writerow(['word', 'category'])

        for word_obj in SensitiveWord.objects.all():
            writer.writerow([word_obj.word, word_obj.category])

        return response

    export_sensitive_words.short_description = '导出敏感词 (CSV)'
