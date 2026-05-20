from django.urls import path, include
from . import views
from . import views_staff_profile

app_name = 'auth'

urlpatterns = [
    path('login/', views.unified_login_view, name='login'),
    path('patient-login/', views.patient_login_view, name='patient_login'),
    path('staff-login/', views.staff_login_view, name='staff_login'),
    path('register/', views.register_view, name='register'),
    path('phone-login/', views.phone_login_view, name='phone_login'),
    path('send-sms/', views.send_sms_code, name='send_sms'),
    path('terms/', views.terms_agreement_view, name='terms_agreement'),
    path('password-change/', views.password_change_view, name='password_change'),
    path('password-reset/send-code/', views.password_reset_send_code, name='password_reset_send_code'),
    path('password-reset/confirm/', views.password_reset_confirm, name='password_reset_confirm'),
    path('password-reset/', views.password_reset_view, name='password_reset'),
    path('logout/', views.logout_view, name='logout'),
    path('settings/', views.settings_index_view, name='settings_index'),
    path('settings/security/', views.security_settings_view, name='security_settings'),
    path('settings/privacy/', views.privacy_settings_view, name='privacy_settings'),
    path('settings/phone/', views.phone_binding_view, name='phone_binding'),
    path('settings/avatar/', views.avatar_upload_view, name='avatar_upload'),
    path('settings/preferences/', views.preferences_view, name='preferences'),
    path('profile/', views.profile_view, name='profile'),
    path('select-departments/', views.select_departments, name='select_departments'),
    path('', include(('apps.auth.bind.urls', 'bind'), namespace='bind')),
]

staff_profile_urlpatterns = [
    path('', views_staff_profile.staff_profile_view, name='profile'),
    path('edit/', views_staff_profile.staff_profile_edit_view, name='profile_edit'),
    path('security/', views_staff_profile.staff_profile_security_view, name='profile_security'),
]

staff_urlpatterns = [
    path('login/', views.staff_login_view, name='login'),
    path('dashboard/', views.staff_dashboard_view, name='dashboard'),
    path('patients/', include('apps.care.staff_urls')),
    path('profile/', include(staff_profile_urlpatterns)),
]
