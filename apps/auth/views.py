import json
import logging
import re
from django.contrib.auth import get_user_model, login, logout
from django.contrib.auth.decorators import login_required
from django.contrib import messages
from django.shortcuts import render, redirect
from django.urls import reverse
from django.utils.translation import gettext_lazy as _
from django.utils.http import url_has_allowed_host_and_scheme
from django.views.decorators.http import require_POST
from django.http import JsonResponse
from .decorators import staff_member_required, role_aware_login_required

from .forms import (
    H5LoginForm, PhoneLoginForm, RegistrationForm,
    H5PasswordChangeForm, TermsAgreementForm,
    PhoneBindingForm, PhoneUnbindForm,
    SecuritySettingsForm, PrivacySettingsForm,
    AvatarUploadForm, PreferencesForm,
)
from apps.base.logging import get_audit_logger
from apps.service_container import container
from .middleware import SingleSessionMiddleware

logger = logging.getLogger(__name__)
User = get_user_model()


def _get_safe_next(request, user=None):
    next_url = request.GET.get('next', '/')
    if not next_url or not url_has_allowed_host_and_scheme(url=next_url, allowed_hosts={request.get_host()}):
        return '/'

    if user and user.is_authenticated:
        staff_only_prefixes = ('/staff/',)
        patient_only_prefixes = ('/chat/', '/profile/',)

        if user.is_patient:
            for prefix in staff_only_prefixes:
                if next_url.startswith(prefix):
                    return '/'

        elif user.can_access_staff_dashboard:
            for prefix in patient_only_prefixes:
                if next_url.startswith(prefix):
                    return '/'

    return next_url


def _get_redirect_by_role(user):
    if user.is_admin:
        return '/admin/'
    elif user.is_medical_staff:
        return '/accounts/staff-login/'
    return '/accounts/patient-login/?role=patient'


def _process_login(request, user, success_redirect: str, success_message: str = None):
    from django.conf import settings as django_settings
    from .services.session_security_service import destroy_old_session, clear_session_before_login

    audit = get_audit_logger()
    ip = request.META.get('REMOTE_ADDR', 'unknown')
    user_agent = request.META.get('HTTP_USER_AGENT', '')

    old_session_key = user.current_session_key

    clear_session_before_login(request)
    login(request, user)

    request.session['user_role'] = user.role

    destroy_old_session(old_session_key)

    user.current_session_key = request.session.session_key
    user.save(update_fields=['current_session_key'])
    SingleSessionMiddleware.invalidate_session_cache(user.pk)
    container.login_security_service.clear_failed_attempts(user.id)
    audit.log_login(user.id, user.username, ip, True)
    container.user_settings_service.record_login_device(user, ip, user_agent)

    if getattr(django_settings, 'CHAT_ANONYMOUS_MIGRATION_ENABLED', True) and user.is_patient:
        try:
            container.chat_management_service.migrate_anonymous_conversations(old_session_key, user)
        except Exception:
            pass

    messages.success(request, success_message or _("登录成功"))
    return redirect(success_redirect)


def _handle_login_failure(request, username: str):
    audit = get_audit_logger()
    ip = request.META.get('REMOTE_ADDR', 'unknown')
    audit.log_login_failure(username, ip, 'invalid_credentials')
    login_security = container.login_security_service
    login_security.handle_failed_login(username)
    if username and not username.isalpha():
        login_security.handle_failed_login_by_phone(username)
    messages.error(request, _("账号或密码错误，请重试"))


def _redirect_authenticated_user(request):
    if not request.user.is_authenticated:
        return None
    if request.user.is_admin:
        return redirect('/admin/')
    elif request.user.can_access_staff_dashboard:
        return redirect('staff:dashboard')
    return redirect('care:profile_detail')


def unified_login_view(request):
    redirect_response = _redirect_authenticated_user(request)
    if redirect_response:
        return redirect_response

    role = request.GET.get('role', 'patient')
    if role not in ['patient', 'staff']:
        role = 'patient'

    return render(request, 'registration/unified_login.html', {'active_role': role})


def patient_login_view(request):
    if request.method == 'GET':
        redirect_response = _redirect_authenticated_user(request)
        if redirect_response:
            return redirect_response
        return render(request, 'registration/unified_login.html', {'active_role': 'patient'})

    redirect_response = _redirect_authenticated_user(request)
    if redirect_response:
        return redirect_response

    form = H5LoginForm(request, data=request.POST)
    if form.is_valid():
        user = form.get_user()
        if not user.can_access_frontend:
            messages.error(request, _("该账户非患者账户，请使用对应入口登录"))
            return redirect(_get_redirect_by_role(user))
        return _process_login(request, user, _get_safe_next(request, user))
    else:
        _handle_login_failure(request, request.POST.get('username', ''))
        return redirect('auth:patient_login')


def staff_login_view(request):
    if request.user.is_authenticated:
        return redirect('staff:dashboard')

    if request.method == 'POST':
        form = H5LoginForm(request, data=request.POST)
        if form.is_valid():
            user = form.get_user()
            if user.is_admin:
                messages.error(request, _("管理员请使用管理后台登录"))
                return redirect('/admin/')
            if not user.can_access_staff_dashboard:
                if user.is_patient:
                    messages.error(request, _("患者请使用患者端登录"))
                    return redirect('auth:login')
                messages.error(request, _("无权限访问此页面"))
                return redirect('auth:login')
            return _process_login(request, user, 'staff:dashboard')
        else:
            _handle_login_failure(request, request.POST.get('username', ''))
            return redirect('auth:staff_login')

    return render(request, 'registration/unified_login.html', {'active_role': 'staff'})


def phone_login_view(request):
    redirect_response = _redirect_authenticated_user(request)
    if redirect_response:
        return redirect_response

    if request.method == 'POST':
        form = PhoneLoginForm(request.POST)
        if form.is_valid():
            phone = form.cleaned_data['phone']
            code = form.cleaned_data['verification_code']

            user = container.user_service.get_user_by_phone(phone)
            login_security = container.login_security_service
            if user and login_security.is_account_locked(user.id):
                remaining = login_security.get_lock_remaining_minutes(user.id)
                form.add_error(None, _("账户已锁定，请 {} 分钟后再试").format(remaining))
            elif container.sms_service.verify_code(phone, code):
                if user:
                    if not user.can_access_frontend:
                        messages.error(request, _("该账户非患者账户，请使用对应入口登录"))
                        return redirect(_get_redirect_by_role(user))
                    return _process_login(request, user, _get_safe_next(request, user))
                form.add_error('phone', _("该手机号未注册"))
            else:
                if user:
                    login_security.handle_failed_login_by_phone(phone)
                form.add_error('verification_code', _("验证码错误或已过期"))
    else:
        form = PhoneLoginForm()

    return render(request, 'registration/phone_login.html', {'form': form})


@require_POST
def send_sms_code(request):
    try:
        data = json.loads(request.body)
        phone = data.get('phone', '')
        phone_type = data.get('type', 'login')
    except json.JSONDecodeError:
        return JsonResponse({'success': False, 'error': _('无效请求')}, status=400)

    digits = re.sub(r'\D', '', phone)
    if len(digits) < 11:
        return JsonResponse({'success': False, 'error': _('请输入有效的手机号')}, status=400)

    ip = request.META.get('REMOTE_ADDR', 'unknown')
    result = container.sms_service.send_code(digits, ip)
    if not result['success']:
        status = 429 if '频繁' in result.get('error', '') else 400
        return JsonResponse({'success': False, 'error': result['error']}, status=status)

    response_data = {'success': True, 'message': _('验证码已发送')}
    if result.get('is_test_code'):
        response_data['debug_code'] = result['code']
    return JsonResponse(response_data)


def register_view(request):
    redirect_response = _redirect_authenticated_user(request)
    if redirect_response:
        return redirect_response

    if request.method == 'POST':
        form = RegistrationForm(request.POST)
        if form.is_valid():
            try:
                user = form.save()
            except Exception:
                logger.exception("Registration failed for username=%s", request.POST.get('username'))
                messages.error(request, _("注册失败，请稍后重试"))
                return render(request, 'registration/register.html', {'form': form})
            return _process_login(request, user, 'care:profile_edit', _("注册成功，欢迎加入！"))
    else:
        form = RegistrationForm()

    return render(request, 'registration/register.html', {'form': form})


def terms_agreement_view(request):
    if request.user.is_authenticated and request.user.agreed_terms:
        form = None
    elif request.method == 'POST':
        if not request.user.is_authenticated:
            return redirect('login')

        action = request.POST.get('action', 'agree')
        if action == 'reject':
            from django.contrib.auth import logout as auth_logout
            auth_logout(request)
            messages.info(request, _("您已拒绝免责条款，已退出登录"))
            return redirect('auth:login')

        form = TermsAgreementForm(request.POST)
        if form.is_valid():
            request.user.agree_to_terms()
            messages.success(request, _("感谢您同意条款，现在可以正常使用系统"))
            return redirect(_get_safe_next(request))
    else:
        form = TermsAgreementForm() if request.user.is_authenticated else None

    return render(request, 'registration/terms.html', {'form': form})


@login_required
def password_change_view(request):
    if request.method == 'POST':
        form = H5PasswordChangeForm(request.user, request.POST)
        if form.is_valid():
            user_role = request.user.role
            form.save()
            messages.success(request, _("密码修改成功，请重新登录"))
            logout(request)
            if user_role == User.Role.PATIENT:
                return redirect('auth:patient_login')
            elif user_role in (User.Role.DOCTOR, User.Role.NURSE, User.Role.SUPER_ADMIN, User.Role.DEPT_ADMIN):
                return redirect('auth:staff_login')
            return redirect('auth:login')
    else:
        form = H5PasswordChangeForm(request.user)

    return render(request, 'registration/password_change.html', {'form': form})


@login_required
@require_POST
def logout_view(request):
    user = request.user
    user_role = getattr(user, 'role', None)
    user_id = user.pk

    request.session.flush()
    logout(request)

    if user_id:
        User.objects.filter(pk=user_id).update(current_session_key=None)
        SingleSessionMiddleware.invalidate_session_cache(user_id)

    messages.info(request, _("您已安全退出"))

    if user_role == User.Role.SUPER_ADMIN or user_role == User.Role.DEPT_ADMIN:
        return redirect('/admin/')
    elif user_role == User.Role.PATIENT:
        return redirect('auth:patient_login')
    elif user_role in (User.Role.DOCTOR, User.Role.NURSE):
        return redirect('auth:staff_login')

    return redirect('auth:login')


def password_reset_view(request):
    redirect_response = _redirect_authenticated_user(request)
    if redirect_response:
        return redirect_response
    return render(request, 'registration/password_reset.html')


def password_reset_send_code(request):
    if request.method != 'POST':
        return JsonResponse({'error': 'Method not allowed'}, status=405)

    phone = request.POST.get('phone', '').strip()
    if not phone or len(phone) < 11:
        return JsonResponse({'error': '请输入有效的手机号'}, status=400)

    ip = request.META.get('REMOTE_ADDR', 'unknown')
    result = container.sms_service.send_code(phone, ip)
    if result['success']:
        return JsonResponse({'message': '验证码已发送'})
    status_code = 429 if '频繁' in result.get('error', '') else 400
    return JsonResponse({'error': result.get('error')}, status=status_code)


def password_reset_confirm(request):
    if request.method != 'POST':
        return JsonResponse({'error': 'Method not allowed'}, status=405)

    phone = request.POST.get('phone', '').strip()
    code = request.POST.get('code', '').strip()
    new_password = request.POST.get('new_password', '')
    confirm_password = request.POST.get('confirm_password', '')

    if not phone or not code or not new_password or not confirm_password:
        return JsonResponse({'error': '请填写所有字段'}, status=400)

    if new_password != confirm_password:
        return JsonResponse({'error': '两次密码输入不一致'}, status=400)

    if len(new_password) < 8:
        return JsonResponse({'error': '密码至少 8 位'}, status=400)

    from django.contrib.auth.password_validation import validate_password
    from django.core.exceptions import ValidationError
    try:
        validate_password(new_password)
    except ValidationError as e:
        if hasattr(e, 'messages') and e.messages:
            error_msg = e.messages[0]
        elif hasattr(e, 'message'):
            error_msg = e.message
        else:
            error_msg = str(e)
        return JsonResponse({'error': error_msg}, status=400)

    from django.core.cache import cache
    from django.utils import timezone

    reset_key = f"password_reset_used:{phone}"
    reset_data = cache.get(reset_key, {'count': 0, 'first_at': None})
    if reset_data['count'] >= 3:
        return JsonResponse({'error': '密码重置次数已达上限，请1小时后再试'}, status=429)

    if not container.sms_service.verify_code(phone, code):
        return JsonResponse({'error': '验证码错误或已过期'}, status=400)

    user = container.user_service.get_user_by_phone(phone)
    if not user:
        return JsonResponse({'error': '该账号未绑定手机号，无法重置'}, status=400)

    user.set_password(new_password)
    user.save(update_fields=['password'])

    ip = request.META.get('REMOTE_ADDR', 'unknown')
    audit = get_audit_logger()
    audit.log_data_access(
        user_id=user.id, username=user.username,
        data_type='password', target_id=str(user.id),
        action='reset', ip=ip
    )
    logger.info("Password reset for user_id=%s username=%s from ip=%s", user.id, user.username, ip)

    now = timezone.now().timestamp()
    cache.set(reset_key, {'count': reset_data['count'] + 1, 'first_at': reset_data['first_at'] or now}, 3600)

    messages.success(request, '密码已重置，请使用新密码登录')
    return JsonResponse({'message': '密码已重置', 'redirect_url': reverse('auth:login')})


@login_required
def settings_index_view(request):
    user = request.user
    context = {
        'user': user,
        'phone': user.phone or '',
        'is_phone_bound': bool(user.phone),
        'login_alert_enabled': user.login_alert_enabled,
        'health_data_share_enabled': user.health_data_share_enabled,
    }
    return render(request, 'profile/settings.html', context)


@login_required
def security_settings_view(request):
    user = request.user
    if request.method == 'POST':
        form = SecuritySettingsForm(request.POST)
        if form.is_valid():
            container.user_settings_service.update_security_settings(
                user,
                login_alert_enabled=form.cleaned_data.get('login_alert_enabled', False)
            )
            messages.success(request, _("安全设置已更新"))
            return redirect('auth:security_settings')
    else:
        form = SecuritySettingsForm(initial={
            'login_alert_enabled': user.login_alert_enabled
        })
    context = {
        'form': form,
        'login_alert_enabled': user.login_alert_enabled,
    }
    return render(request, 'profile/security_settings.html', context)


@login_required
def privacy_settings_view(request):
    user = request.user
    if request.method == 'POST':
        form = PrivacySettingsForm(request.POST)
        if form.is_valid():
            container.user_settings_service.update_privacy_settings(
                user,
                health_data_share_enabled=form.cleaned_data.get('health_data_share_enabled', False)
            )
            messages.success(request, _("隐私设置已更新"))
            return redirect('auth:privacy_settings')
    else:
        form = PrivacySettingsForm(initial={
            'health_data_share_enabled': user.health_data_share_enabled
        })
    context = {
        'form': form,
        'health_data_sharing_enabled': user.health_data_share_enabled,
    }
    return render(request, 'profile/privacy_settings.html', context)


@login_required
def phone_binding_view(request):
    user = request.user
    is_phone_bound = bool(user.phone)
    current_phone = user.phone or ''

    binding_form = PhoneBindingForm()
    unbinding_form = PhoneUnbindForm()

    if request.method == 'POST':
        if is_phone_bound:
            unbinding_form = PhoneUnbindForm(request.POST)
            if unbinding_form.is_valid():
                if container.user_settings_service.unbind_phone(user, unbinding_form.cleaned_data['verification_code']):
                    messages.success(request, _("手机号已解除绑定"))
                else:
                    messages.error(request, _("验证码错误"))
                return redirect('auth:phone_binding')
        else:
            binding_form = PhoneBindingForm(request.POST)
            if binding_form.is_valid():
                if container.user_settings_service.bind_phone(
                    user,
                    binding_form.cleaned_data['phone'],
                    binding_form.cleaned_data['verification_code']
                ):
                    messages.success(request, _("手机号绑定成功"))
                    return redirect('auth:phone_binding')
                else:
                    messages.error(request, _("验证码错误"))

    context = {
        'phone': current_phone,
        'is_phone_bound': is_phone_bound,
        'binding_form': binding_form,
        'unbinding_form': unbinding_form,
    }
    return render(request, 'profile/phone_binding.html', context)


@login_required
def avatar_upload_view(request):
    user = request.user
    if request.method == 'POST':
        form = AvatarUploadForm(request.POST, request.FILES)
        if form.is_valid():
            container.user_settings_service.update_avatar(user, form.cleaned_data['avatar'])
            messages.success(request, _("头像上传成功"))
            return redirect('auth:settings_index')
    else:
        form = AvatarUploadForm()

    context = {
        'form': form,
        'current_avatar': user.avatar,
    }
    return render(request, 'profile/avatar_upload.html', context)


@login_required
def preferences_view(request):
    user = request.user
    if request.method == 'POST':
        form = PreferencesForm(request.POST)
        if form.is_valid():
            preferences = {
                'theme': form.cleaned_data.get('theme', 'light'),
                'language': form.cleaned_data.get('language', 'zh-CN'),
                'notification_enabled': form.cleaned_data.get('notification_enabled', True),
                'font_size': form.cleaned_data.get('font_size', 'medium'),
            }
            container.user_settings_service.update_user_preferences(user, preferences)
            messages.success(request, _("偏好设置已更新"))
            return redirect('auth:preferences')
    else:
        current_prefs = container.user_settings_service.get_user_preferences(user)
        form = PreferencesForm(initial=current_prefs)

    context = {
        'form': form,
    }
    return render(request, 'profile/preferences.html', context)


@login_required
@require_POST
def select_departments(request):
    dept_ids = request.POST.getlist('departments')
    from apps.base.models import Department
    depts = Department.objects.filter(id__in=dept_ids) if dept_ids else Department.objects.none()

    if hasattr(request.user, 'patient_profile'):
        non_leaf = [d.name for d in depts if d.children.exists()]
        if non_leaf:
            error_msg = f"只能将患者分配到叶子科室，以下科室包含子科室: {', '.join(non_leaf)}"
            if request.headers.get('x-requested-with') == 'XMLHttpRequest':
                return JsonResponse({'status': 'error', 'error': error_msg}, status=400)
            from django.contrib import messages
            messages.error(request, error_msg)
            return redirect('care:profile_detail')
        request.user.patient_profile.departments.set(depts)

    if request.headers.get('x-requested-with') == 'XMLHttpRequest':
        return JsonResponse({'status': 'ok', 'departments': dept_ids})
    return redirect('care:profile_detail')


@login_required
def profile_view(request):
    if request.user.can_access_staff_dashboard:
        return redirect('staff:profile')
    return redirect('care:profile_detail')


@role_aware_login_required
@staff_member_required
def staff_dashboard_view(request):
    dashboard_service = container.staff_dashboard_service
    context = dashboard_service.get_dashboard_context(request.user, request)
    return render(request, 'staff/dashboard.html', context)
