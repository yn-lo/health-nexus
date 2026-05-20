"""
医护端患者管理 URL 配置
"""
from django.urls import path
from .views_staff import (
    patient_list_view,
    patient_search_view,
    patient_detail_view,
    bound_patient_list_view,
    staff_vital_sign_create,
    staff_lab_test_create,
    staff_imaging_create,
    staff_medication_create,
)

app_name = 'care'

urlpatterns = [
    path('', patient_list_view, name='staff_patient_list'),
    path('search/', patient_search_view, name='staff_patient_search'),
    path('bound/', bound_patient_list_view, name='staff_bound_patients'),
    path('<int:pk>/', patient_detail_view, name='staff_patient_detail'),
    path('<int:pk>/vitals/create/', staff_vital_sign_create, name='staff_vital_sign_create'),
    path('<int:pk>/lab-tests/create/', staff_lab_test_create, name='staff_lab_test_create'),
    path('<int:pk>/imaging/create/', staff_imaging_create, name='staff_imaging_create'),
    path('<int:pk>/medications/create/', staff_medication_create, name='staff_medication_create'),
]
