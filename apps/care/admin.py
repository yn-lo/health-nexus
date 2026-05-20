from django.contrib import admin
from unfold.admin import ModelAdmin
from .models import PatientProfile, VitalSignRecord, LabTestRecord, ImagingRecord, MedicationRecord


@admin.register(PatientProfile)
class PatientProfileAdmin(ModelAdmin):
    list_display = ['user', 'name', 'age', 'gender', 'departments_display', 'created_at']
    list_filter = ['gender', 'created_at']
    search_fields = ['user__username', 'user__email', 'name']
    ordering = ['-created_at']
    filter_horizontal = ['departments']

    fieldsets = (
        ('基本信息', {
            'fields': ('user', 'name', 'gender', 'birth_date')
        }),
        ('健康信息', {
            'fields': ('medical_history_summary', 'allergies_summary'),
            'classes': ('collapse',)
        }),
        ('就诊科室', {
            'fields': ('departments',)
        }),
    )

    def departments_display(self, obj):
        departments = obj.departments.all()
        return ', '.join([d.name for d in departments]) if departments else '-'
    departments_display.short_description = '就诊科室'


@admin.register(VitalSignRecord)
class VitalSignRecordAdmin(ModelAdmin):
    list_display = ['patient', 'record_date', 'systolic_bp', 'diastolic_bp', 'heart_rate', 'created_at']
    list_filter = ['record_date', 'measurement_context']
    search_fields = ['patient__name', 'source']
    ordering = ['-record_date', '-record_time']


@admin.register(LabTestRecord)
class LabTestRecordAdmin(ModelAdmin):
    list_display = ['patient', 'test_date', 'lab_type', 'source', 'created_at']
    list_filter = ['test_date', 'lab_type']
    search_fields = ['patient__name', 'source']
    ordering = ['-test_date']


@admin.register(ImagingRecord)
class ImagingRecordAdmin(ModelAdmin):
    list_display = ['patient', 'exam_date', 'imaging_type', 'body_part', 'source', 'created_at']
    list_filter = ['exam_date', 'imaging_type']
    search_fields = ['patient__name', 'source', 'body_part']
    ordering = ['-exam_date']


@admin.register(MedicationRecord)
class MedicationRecordAdmin(ModelAdmin):
    list_display = ['patient', 'medication_name', 'status', 'start_date', 'end_date', 'created_at']
    list_filter = ['status', 'route']
    search_fields = ['patient__name', 'medication_name']
    ordering = ['-start_date']
