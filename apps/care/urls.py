from django.urls import path
from . import views

app_name = 'care'


urlpatterns = [
    path('', views.profile_detail, name='profile_detail'),
    path('basic-info/', views.basic_info_edit, name='basic_info_edit'),
    path('edit/', views.profile_edit, name='profile_edit'),

    # 统一医疗记录列表
    path('medical-records/', views.medical_record_list, name='medical_record_list'),

    # 生命体征
    path('vitals/', views.vital_sign_list, name='vital_sign_list'),
    path('vitals/create/', views.vital_sign_create, name='vital_sign_create'),
    path('vitals/<uuid:record_id>/delete/', views.vital_sign_delete, name='vital_sign_delete'),
    path('vitals/trend/', views.vital_sign_trend, name='vital_sign_trend'),

    # 检验报告
    path('lab-tests/', views.lab_test_list, name='lab_test_list'),
    path('lab-tests/create/', views.lab_test_create, name='lab_test_create'),
    path('lab-tests/<uuid:record_id>/delete/', views.lab_test_delete, name='lab_test_delete'),

    # 影像检查
    path('imaging/', views.imaging_list, name='imaging_list'),
    path('imaging/create/', views.imaging_create, name='imaging_create'),
    path('imaging/<uuid:record_id>/delete/', views.imaging_delete, name='imaging_delete'),

    # 用药记录
    path('medications/', views.medication_list, name='medication_list'),
    path('medications/create/', views.medication_create, name='medication_create'),
    path('medications/<uuid:record_id>/stop/', views.medication_stop, name='medication_stop'),
    path('medications/<uuid:record_id>/resume/', views.medication_resume, name='medication_resume'),
    path('medications/<uuid:record_id>/delete/', views.medication_delete, name='medication_delete'),
]
