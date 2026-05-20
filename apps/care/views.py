from django.shortcuts import render, redirect
from django.http import JsonResponse
from django.contrib import messages
from django.core.exceptions import PermissionDenied
from django.utils import timezone
from django.views.decorators.http import require_POST
from datetime import datetime
from .forms import PatientProfileH5Form, BasicInfoForm, COMMON_DISEASES, COMMON_ALLERGIES
from .forms import VitalSignForm, LabTestForm, ImagingForm, MedicationRecordForm
from apps.care.services import (
    PatientService, VitalSignService, LabTestService, ImagingService,
    MedicalRecordAggregator, MedicationService
)
from apps.auth.decorators import patient_only_required


def _get_patient_profile(request):
    patient_svc = PatientService()
    return patient_svc.get_patient_profile(request.user)


@patient_only_required
def profile_detail(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:basic_info_edit")

    patient_svc = PatientService()
    context = patient_svc.get_profile_detail_context(profile, request.user)
    return render(request, "profile/detail.html", context)


@patient_only_required
def basic_info_edit(request):
    profile = _get_patient_profile(request)

    if request.method == "POST":
        form = BasicInfoForm(request.POST, instance=profile)
        if form.is_valid():
            if not profile:
                new_profile = form.save(commit=False)
                new_profile.user = request.user
                new_profile.save()
            else:
                form.save()

            messages.success(request, "基础信息已更新")
            return redirect("care:profile_detail")
    else:
        form = BasicInfoForm(instance=profile)

    return render(
        request,
        "profile/basic_info.html",
        {"form": form},
    )


@patient_only_required
def profile_edit(request):
    profile = _get_patient_profile(request)

    if request.method == "POST":
        form = PatientProfileH5Form(request.POST, instance=profile)
        if form.is_valid():
            if not profile:
                new_profile = form.save(commit=False)
                new_profile.user = request.user
                new_profile.save()
                form.save_m2m()
            else:
                form.save()

            messages.success(request, "档案已更新")
            return redirect("care:profile_detail")
    else:
        form = PatientProfileH5Form(instance=profile)

    return render(
        request,
        "profile/edit.html",
        {
            "form": form,
            "common_diseases": COMMON_DISEASES,
            "common_allergies": COMMON_ALLERGIES,
        },
    )


@patient_only_required
def medical_record_list(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    aggregator = MedicalRecordAggregator()
    records = aggregator.get_all_records(profile)
    return render(request, "care/medical_record_list.html", {"records": records, "profile": profile})


@patient_only_required
def vital_sign_list(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = VitalSignService()
    records = svc.get_vital_signs_by_patient(profile)
    return render(request, "care/vital_sign_list.html", {"records": records, "profile": profile})


@patient_only_required
def vital_sign_create(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    if request.method == "POST":
        form = VitalSignForm(request.POST)
        if form.is_valid():
            svc = VitalSignService()
            try:
                svc.create_vital_sign(patient=profile, data=form.cleaned_data)
                messages.success(request, "生命体征记录已添加")
                return redirect("care:vital_sign_list")
            except Exception as e:
                messages.error(request, str(e))
    else:
        form = VitalSignForm()

    return render(request, "care/vital_sign_form.html", {"form": form, "profile": profile, "title": "新增生命体征"})


@patient_only_required
@require_POST
def vital_sign_delete(request, record_id):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = VitalSignService()
    try:
        svc.delete_vital_sign(record_id, request.user)
        messages.success(request, "记录已删除")
    except PermissionDenied:
        messages.error(request, "无权删除他人的生命体征记录")
    return redirect("care:vital_sign_list")


@patient_only_required
def vital_sign_trend(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    if request.headers.get('Accept', '').find('application/json') != -1 or request.GET:
        indicator = request.GET.get('indicator', 'blood_pressure')
        days = int(request.GET.get('days', 7))
        svc = VitalSignService()
        return JsonResponse(svc.get_vital_sign_trend(profile, indicator=indicator, days=days), safe=False)

    return render(request, 'care/vital_sign_trend.html')


@patient_only_required
def lab_test_list(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = LabTestService()
    records = svc.get_lab_tests_by_patient(profile)
    return render(request, "care/lab_test_list.html", {"records": records, "profile": profile})


@patient_only_required
def lab_test_create(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    if request.method == "POST":
        form = LabTestForm(request.POST)
        if form.is_valid():
            svc = LabTestService()
            try:
                data = {
                    'lab_type': form.cleaned_data['lab_type'],
                    'test_date': form.cleaned_data.get('test_date'),
                    'source': form.cleaned_data.get('source', ''),
                    'data': form.cleaned_data.get('data_json', {}),
                    'notes': form.cleaned_data.get('notes', ''),
                }
                svc.create_lab_test(patient=profile, data=data)
                messages.success(request, "检验报告已添加")
                return redirect("care:lab_test_list")
            except Exception as e:
                messages.error(request, str(e))
    else:
        form = LabTestForm()

    return render(request, "care/lab_test_form.html", {"form": form, "profile": profile, "title": "新增检验报告"})


@patient_only_required
@require_POST
def lab_test_delete(request, record_id):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = LabTestService()
    try:
        svc.delete_lab_test(record_id, request.user)
        messages.success(request, "检验报告已删除")
    except PermissionDenied:
        messages.error(request, "无权删除他人的检验报告")
    return redirect("care:lab_test_list")


@patient_only_required
def imaging_list(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = ImagingService()
    records = svc.get_imaging_by_patient(profile)
    return render(request, "care/imaging_list.html", {"records": records, "profile": profile})


@patient_only_required
def imaging_create(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    if request.method == "POST":
        form = ImagingForm(request.POST)
        if form.is_valid():
            svc = ImagingService()
            try:
                svc.create_imaging(patient=profile, data=form.cleaned_data)
                messages.success(request, "影像检查已添加")
                return redirect("care:imaging_list")
            except Exception as e:
                messages.error(request, str(e))
    else:
        form = ImagingForm()

    return render(request, "care/imaging_form.html", {"form": form, "profile": profile, "title": "新增影像检查"})


@patient_only_required
@require_POST
def imaging_delete(request, record_id):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = ImagingService()
    try:
        svc.delete_imaging(record_id, request.user)
        messages.success(request, "影像检查已删除")
    except PermissionDenied:
        messages.error(request, "无权删除他人的影像检查记录")
    return redirect("care:imaging_list")


@patient_only_required
def medication_list(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = MedicationService()
    status_filter = request.GET.get('status', 'active')
    if status_filter == 'all':
        medications = svc.get_medication_history(profile)
    else:
        medications = svc.get_current_medications(profile)

    return render(request, "care/medication_list.html", {
        "medications": medications,
        "profile": profile,
        "status_filter": status_filter,
        "today": timezone.now().date().isoformat(),
    })


@patient_only_required
def medication_create(request):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    if request.method == "POST":
        form = MedicationRecordForm(request.POST)
        if form.is_valid():
            svc = MedicationService()
            try:
                svc.create_medication(patient=profile, data=form.cleaned_data)
                messages.success(request, "用药记录已添加")
                return redirect("care:medication_list")
            except Exception as e:
                messages.error(request, str(e))
    else:
        form = MedicationRecordForm()

    return render(request, "care/medication_form.html", {"form": form, "profile": profile, "title": "新增用药记录"})


@patient_only_required
@require_POST
def medication_stop(request, record_id):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = MedicationService()
    try:
        end_date_str = request.POST.get('end_date')
        end_date = None
        if end_date_str:
            end_date = datetime.strptime(end_date_str, '%Y-%m-%d').date()
        svc.stop_medication(record_id, request.user, end_date=end_date)
        messages.success(request, "已停药")
    except Exception as e:
        messages.error(request, str(e))
    return redirect("care:medication_list")


@patient_only_required
@require_POST
def medication_resume(request, record_id):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = MedicationService()
    try:
        svc.resume_medication(record_id, request.user)
        messages.success(request, "已恢复用药")
    except Exception as e:
        messages.error(request, str(e))
    return redirect("care:medication_list")


@patient_only_required
@require_POST
def medication_delete(request, record_id):
    profile = _get_patient_profile(request)
    if not profile:
        return redirect("care:profile_edit")

    svc = MedicationService()
    try:
        svc.delete_medication(record_id, request.user)
        messages.success(request, "用药记录已删除")
    except PermissionDenied:
        messages.error(request, "无权删除他人的用药记录")
    return redirect("care:medication_list")
