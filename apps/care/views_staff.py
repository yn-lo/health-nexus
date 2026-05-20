from django.shortcuts import render, get_object_or_404, redirect
from django.contrib.auth.decorators import login_required
from django.contrib import messages
from apps.auth.decorators import staff_member_required
from apps.auth.bind.models import PatientDoctorBinding
from apps.care.models import PatientProfile
from apps.care.forms import VitalSignForm, LabTestForm, ImagingForm, MedicationRecordForm
from apps.care import queries as care_queries


def _check_doctor_patient_access(doctor, patient):
    user_dept_ids = doctor.departments.values_list('id', flat=True)
    if patient.departments.filter(id__in=user_dept_ids).exists():
        return True
    return PatientDoctorBinding.objects.filter(
        doctor=doctor,
        patient=patient.user,
        status=PatientDoctorBinding.Status.CONFIRMED,
    ).exists()


@login_required
@staff_member_required
def patient_list_view(request):
    user = request.user
    department = user.departments.first()

    page = int(request.GET.get('page', 1))
    page_size = 20

    result = care_queries.get_patients_by_department_paginated(
        department.id, page=page, page_size=page_size
    ) if department else care_queries.PaginatedResult([], 0, page, page_size)

    return render(request, 'staff/care/patient_list.html', {
        'patients': result.items,
        'department': department,
        'page': result.page,
        'total_count': result.total_count,
        'has_next': result.has_next,
        'has_prev': result.has_prev,
    })


@login_required
@staff_member_required
def patient_search_view(request):
    user = request.user
    department = user.departments.first()
    query = request.GET.get('q', '').strip()

    page = int(request.GET.get('page', 1))
    page_size = 20

    result = care_queries.search_patients(
        query,
        department_id=department.id if department else None,
        page=page,
        page_size=page_size,
    )

    return render(request, 'staff/care/partials/_patient_table.html', {
        'patients': result.items,
        'search_query': query,
        'department': department,
        'is_search': True,
        'page': result.page,
        'total_count': result.total_count,
        'has_next': result.has_next,
        'has_prev': result.has_prev,
    })


@login_required
@staff_member_required
def patient_detail_view(request, pk):
    patient = get_object_or_404(PatientProfile, pk=pk)

    if not _check_doctor_patient_access(request.user, patient):
        messages.error(request, '您没有权限查看此患者档案')
        return redirect('staff:care:staff_patient_list')

    return render(request, 'staff/care/patient_detail.html', {
        'patient': patient,
    })


@login_required
@staff_member_required
def bound_patient_list_view(request):
    user = request.user
    bindings = PatientDoctorBinding.objects.filter(
        doctor=user,
        status=PatientDoctorBinding.Status.CONFIRMED,
    ).select_related('patient__patient_profile').order_by('-confirmed_at')

    patients = []
    for binding in bindings:
        try:
            profile = binding.patient.patient_profile
            patients.append({
                'profile': profile,
                'binding': binding,
                'bound_at': binding.confirmed_at,
            })
        except PatientProfile.DoesNotExist:
            continue

    return render(request, 'staff/care/bound_patient_list.html', {
        'patients': patients,
        'total_count': len(patients),
    })


@login_required
@staff_member_required
def staff_vital_sign_create(request, pk):
    patient = get_object_or_404(PatientProfile, pk=pk)
    if not _check_doctor_patient_access(request.user, patient):
        messages.error(request, '您没有权限为此患者录入数据')
        return redirect('staff:care:staff_patient_list')

    if request.method == 'POST':
        form = VitalSignForm(request.POST)
        if form.is_valid():
            record = form.save(commit=False)
            record.patient = patient
            record.save()
            messages.success(request, '体征数据已录入')
            return redirect('staff:care:staff_patient_detail', pk=patient.pk)
    else:
        form = VitalSignForm()

    return render(request, 'staff/care/record_form.html', {
        'patient': patient,
        'form': form,
        'title': '录入体征数据',
    })


@login_required
@staff_member_required
def staff_lab_test_create(request, pk):
    patient = get_object_or_404(PatientProfile, pk=pk)
    if not _check_doctor_patient_access(request.user, patient):
        messages.error(request, '您没有权限为此患者录入数据')
        return redirect('staff:care:staff_patient_list')

    if request.method == 'POST':
        form = LabTestForm(request.POST)
        if form.is_valid():
            record = form.save(commit=False)
            record.patient = patient
            record.save()
            messages.success(request, '检验报告已录入')
            return redirect('staff:care:staff_patient_detail', pk=patient.pk)
    else:
        form = LabTestForm()

    return render(request, 'staff/care/record_form.html', {
        'patient': patient,
        'form': form,
        'title': '录入检验报告',
    })


@login_required
@staff_member_required
def staff_imaging_create(request, pk):
    patient = get_object_or_404(PatientProfile, pk=pk)
    if not _check_doctor_patient_access(request.user, patient):
        messages.error(request, '您没有权限为此患者录入数据')
        return redirect('staff:care:staff_patient_list')

    if request.method == 'POST':
        form = ImagingForm(request.POST)
        if form.is_valid():
            record = form.save(commit=False)
            record.patient = patient
            record.save()
            messages.success(request, '影像记录已录入')
            return redirect('staff:care:staff_patient_detail', pk=patient.pk)
    else:
        form = ImagingForm()

    return render(request, 'staff/care/record_form.html', {
        'patient': patient,
        'form': form,
        'title': '录入影像记录',
    })


@login_required
@staff_member_required
def staff_medication_create(request, pk):
    patient = get_object_or_404(PatientProfile, pk=pk)
    if not _check_doctor_patient_access(request.user, patient):
        messages.error(request, '您没有权限为此患者录入数据')
        return redirect('staff:care:staff_patient_list')

    if request.method == 'POST':
        form = MedicationRecordForm(request.POST)
        if form.is_valid():
            record = form.save(commit=False)
            record.patient = patient
            record.save()
            messages.success(request, '用药记录已录入')
            return redirect('staff:care:staff_patient_detail', pk=patient.pk)
    else:
        form = MedicationRecordForm()

    return render(request, 'staff/care/record_form.html', {
        'patient': patient,
        'form': form,
        'title': '录入用药记录',
    })
