from django.urls import path
from . import views

app_name = 'bind'

urlpatterns = [
    path('bind/generate-qr/', views.generate_qr, name='generate_qr'),
    path('bind/requests/', views.list_bind_requests, name='bind_requests'),
    path('bind/requests/<int:binding_id>/confirm/', views.confirm_bind, name='confirm_bind'),
    path('bind/requests/<int:binding_id>/reject/', views.reject_bind, name='reject_bind'),
    path('bind/my/', views.list_my_bindings, name='my_bindings'),
    path('bind/<str:qr_code>/', views.bind_request_view, name='bind_confirm'),
    path('bind/<str:qr_code>/initiate/', views.initiate_bind, name='initiate_bind'),
]
