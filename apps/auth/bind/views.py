import logging
from django.http import JsonResponse
from django.shortcuts import render, redirect
from django.contrib.auth.decorators import login_required
from django.contrib import messages
from apps.service_container import container
from apps.care.models import PatientProfile

logger = logging.getLogger(__name__)


@login_required
def generate_qr(request):
    bind_service = container.binding_service
    qr_code, qr_image = bind_service.generate_user_qr(request.user.id)
    return JsonResponse({
        'qr_code': qr_code,
        'qr_image': qr_image,
        'bind_url': f"/accounts/bind/{qr_code}/",
    })


@login_required
def bind_request_view(request, qr_code):
    bind_service = container.binding_service
    target_info = bind_service.resolve_user_by_qr(qr_code)
    if not target_info:
        return render(request, 'auth/bind_error.html', {
            'error': '无效的绑定二维码'
        })
    return render(request, 'auth/bind_confirm.html', {
        'target': target_info,
        'qr_code': qr_code,
    })


@login_required
def initiate_bind(request, qr_code):
    bind_service = container.binding_service
    target_info = bind_service.resolve_user_by_qr(qr_code)
    if not target_info:
        return render(request, 'auth/bind_error.html', {
            'error': '无效的绑定二维码'
        })

    initiator_type = 'DOCTOR' if request.user.is_medical_staff else 'PATIENT'
    target_role = target_info['role']

    if initiator_type == 'DOCTOR' and target_role != 'PATIENT':
        return render(request, 'auth/bind_error.html', {
            'error': '只能绑定患者'
        })
    elif initiator_type == 'PATIENT' and target_role not in ['DOCTOR', 'NURSE']:
        return render(request, 'auth/bind_error.html', {
            'error': '只能绑定医护人员'
        })

    success, message = bind_service.create_binding_request(
        target_user_id=target_info['id'],
        initiator_user_id=request.user.id,
        initiator_type=initiator_type,
    )
    if success:
        return render(request, 'auth/bind_request_sent.html', {
            'message': message,
            'target': target_info,
        })
    return render(request, 'auth/bind_error.html', {'error': message})


@login_required
def confirm_bind(request, binding_id):
    bind_service = container.binding_service
    success, message = bind_service.confirm_binding(binding_id, request.user.id)
    if success:
        messages.success(request, message)
    else:
        messages.error(request, message)
    return redirect('auth:bind:bind_requests')


@login_required
def reject_bind(request, binding_id):
    bind_service = container.binding_service
    success, message = bind_service.reject_binding(binding_id, request.user.id)
    if success:
        messages.success(request, message)
    else:
        messages.error(request, message)
    return redirect('auth:bind:bind_requests')


@login_required
def list_bind_requests(request):
    bind_service = container.binding_service
    pending = bind_service.get_pending_requests_for_user(request.user.id)

    page = int(request.GET.get('page', 1))
    page_size = 20
    total = len(pending)
    offset = (page - 1) * page_size
    paged_pending = pending[offset:offset + page_size]

    data = []
    for b in paged_pending:
        if request.user.id == b.doctor_id:
            other_user = b.patient
        elif request.user.id == b.patient_id:
            other_user = b.doctor
        else:
            continue

        entry = {
            'id': b.id,
            'other_user': other_user,
            'initiator': b.initiator,
            'initiator_display': b.get_initiator_display(),
            'requested_at': b.requested_at,
            'name': other_user.username,
            'gender': '',
            'age': 0,
            'avatar_url': other_user.avatar.url if other_user.avatar else None,
        }

        if other_user.role == other_user.Role.PATIENT:
            try:
                profile = other_user.patient_profile
                entry['name'] = profile.name or other_user.username
                entry['gender'] = profile.gender
                entry['age'] = profile.age
            except PatientProfile.DoesNotExist:
                pass
            except Exception as e:
                logger.warning(
                    'Error fetching patient profile for user %s: %s',
                    other_user.id, e
                )

        data.append(entry)

    return render(request, 'auth/bind_requests.html', {
        'requests': data,
        'page': page,
        'total_count': total,
        'has_next': page * page_size < total,
        'has_prev': page > 1,
    })


@login_required
def list_my_bindings(request):
    bind_service = container.binding_service
    if request.user.is_medical_staff:
        bindings = bind_service.get_confirmed_bindings_for_doctor(request.user.id)
        data = [{
            'id': b.id,
            'other_user': b.patient,
            'confirmed_at': b.confirmed_at,
        } for b in bindings]
    else:
        bindings = bind_service.get_confirmed_bindings_for_patient(request.user.id)
        data = [{
            'id': b.id,
            'other_user': b.doctor,
            'confirmed_at': b.confirmed_at,
        } for b in bindings]

    page = int(request.GET.get('page', 1))
    page_size = 20
    total = len(data)
    offset = (page - 1) * page_size
    paged_data = data[offset:offset + page_size]

    return render(request, 'auth/my_bindings_h5.html', {
        'bindings': paged_data,
        'page': page,
        'total_count': total,
        'has_next': page * page_size < total,
        'has_prev': page > 1,
    })
