from django.urls import path
from . import views

app_name = 'base'

urlpatterns = [
    path('', views.about_page, name='about_page'),
]

departments_urls = [
    path('', views.department_list, name='department_list'),
    path('create/', views.department_create, name='department_create'),
    path('<int:dept_id>/edit/', views.department_edit, name='department_edit'),
    path('<int:dept_id>/delete/', views.department_delete, name='department_delete'),
    path('<int:dept_id>/members/', views.department_members_list, name='department_members_list'),
    path('<int:dept_id>/members/add/', views.department_member_add, name='department_member_add'),
    path('<int:dept_id>/members/<int:user_id>/remove/', views.department_member_remove, name='department_member_remove'),
    path('<int:dept_id>/config/', views.department_config, name='department_config'),
    path('<int:dept_id>/references/', views.department_references, name='department_references'),
    path('<int:dept_id>/audit-logs/', views.department_audit_logs, name='department_audit_logs'),
    path('tree/', views.department_tree, name='department_tree'),
    path('users/<int:user_id>/audit-logs/', views.user_audit_logs, name='user_audit_logs'),
]
