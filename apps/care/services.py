from typing import List, Dict, Any, Optional
import datetime
from django.core.exceptions import PermissionDenied, ValidationError
from apps.care.models import PatientProfile, VitalSignRecord, LabTestRecord, ImagingRecord, MedicationRecord
from apps.base.models import Department


class PatientService:
    """患者档案 CRUD + 上下文服务"""

    def get_patient_profile(self, user) -> Optional[PatientProfile]:
        try:
            return PatientProfile.objects.get(user=user)
        except PatientProfile.DoesNotExist:
            return None

    def get_patient_profile_by_user(self, user) -> Optional[PatientProfile]:
        return self.get_patient_profile(user)

    def get_patient_context(self, patient: PatientProfile) -> Dict[str, Any]:
        if not patient.user.health_data_share_enabled:
            return {}

        context = {}
        if patient.gender:
            context['gender'] = patient.get_gender_display()
        if patient.age:
            context['age'] = patient.age
        if patient.medical_history_summary:
            context['medical_history'] = patient.medical_history_summary
        if patient.allergies_summary:
            context['allergies'] = patient.allergies_summary

        current_meds = MedicationRecord.objects.filter(
            patient=patient, status='active'
        ).order_by('-start_date')[:10]
        if current_meds:
            context['current_medications'] = [
                {"name": m.medication_name, "dosage": m.dosage, "frequency": m.frequency}
                for m in current_meds
            ]

        abnormal_vitals = self._get_abnormal_vitals(patient)
        if abnormal_vitals:
            context['abnormal_vitals'] = abnormal_vitals

        return context

    VITAL_RANGES = {
        'systolic_bp': ('收缩压', 90, 140),
        'diastolic_bp': ('舒张压', 60, 90),
        'heart_rate': ('心率', 60, 100),
        'temperature': ('体温', 36.1, 37.2),
        'blood_glucose': ('血糖', 3.9, 6.1),
        'blood_oxygen': ('血氧饱和度', 95, 100),
        'respiratory_rate': ('呼吸频率', 12, 20),
    }

    def _get_abnormal_vitals(self, patient: PatientProfile) -> List[Dict[str, Any]]:
        latest = VitalSignRecord.objects.filter(
            patient=patient
        ).order_by('-record_date', '-record_time', '-created_at').first()
        if not latest:
            return []

        abnormals = []
        for field, (name, low, high) in self.VITAL_RANGES.items():
            value = getattr(latest, field, None)
            if value is None:
                continue
            numeric_value = float(value)
            if numeric_value < low or numeric_value > high:
                abnormals.append({
                    "indicator": name,
                    "value": numeric_value,
                    "normal_range": f"{low}-{high}",
                })
        return abnormals

    @staticmethod
    def desensitize(patient: PatientProfile) -> Dict[str, Any]:
        """AI 上下文脱敏：移除所有身份识别信息，仅保留医疗参考数据

        遵循 BR-CARE-07：隐私关闭时返回空字典
        """
        svc = PatientService()
        return svc.get_patient_context(patient)

    def get_profile_detail_context(self, profile: PatientProfile, user) -> Dict[str, Any]:
        """获取档案详情页所需的完整上下文"""
        from apps.base.services import DepartmentService

        user_depts = list(profile.departments.all())
        bound_doctor_ids = []
        if hasattr(user, 'doctor_bindings'):
            bound_doctor_ids = list(
                user.doctor_bindings.filter(status='CONFIRMED')
                .values_list('doctor_id', flat=True)
            )

        recommended_dept_ids = {d.id for d in user_depts}

        recommended_doctor_depts = []
        if bound_doctor_ids:
            recommended_doctor_depts = list(Department.objects.filter(
                users__id__in=bound_doctor_ids
            ).exclude(id__in=recommended_dept_ids).distinct())

        all_depts = list(Department.objects.filter(is_active=True).order_by('parent', 'name'))

        dept_service = DepartmentService()
        dept_tree = dept_service.get_department_tree()

        selected_dept_ids = [str(d.id) for d in user_depts]

        return {
            "profile": profile,
            "user_depts": user_depts,
            "recommended_dept_ids": list(recommended_dept_ids),
            "recommended_doctor_depts": recommended_doctor_depts,
            "all_depts": all_depts,
            "dept_tree": dept_tree,
            "selected_dept_ids": selected_dept_ids,
        }

    def get_patients_by_department(self, department_id: int) -> List[PatientProfile]:
        from apps.care.queries import get_patients_by_department as _get_patients
        return _get_patients(department_id)

    def get_accessible_department_ids(self, user) -> List[int]:
        profile = self.get_patient_profile(user)
        if not profile:
            return []
        dept_ids = list(profile.departments.values_list('id', flat=True))
        parent_ids = list(Department.objects.filter(id__in=dept_ids).exclude(parent__isnull=True).values_list('parent_id', flat=True))
        return list(set(dept_ids + parent_ids))


class VitalSignService:
    """生命体征服务"""

    def create_vital_sign(self, patient: PatientProfile, data: Dict[str, Any]) -> VitalSignRecord:
        indicator_fields = [
            'systolic_bp', 'diastolic_bp', 'heart_rate', 'pulse',
            'temperature', 'respiratory_rate', 'blood_glucose', 'blood_oxygen',
            'weight', 'height', 'pain_score',
        ]
        has_any_value = any(data.get(field) for field in indicator_fields)
        if not has_any_value:
            raise ValueError('请至少填写一项体征指标')

        if not data.get('record_date'):
            data['record_date'] = datetime.date.today()

        return VitalSignRecord.objects.create(patient=patient, **data)

    def get_vital_signs_by_patient(self, patient: PatientProfile) -> List[VitalSignRecord]:
        return list(
            VitalSignRecord.objects
            .filter(patient=patient)
            .order_by('-record_date', '-record_time', '-created_at')
        )

    def delete_vital_sign(self, record_id: int, user) -> bool:
        try:
            record = VitalSignRecord.objects.get(id=record_id)
        except VitalSignRecord.DoesNotExist:
            raise PermissionDenied('生命体征记录不存在')
        if record.patient.user_id != user.id:
            raise PermissionDenied('无权删除他人的生命体征记录')
        record.delete()
        return True

    def get_vital_sign_trend(self, patient: PatientProfile, indicator: str, days: int = 7) -> Dict:
        import datetime
        end_date = datetime.date.today()
        start_date = end_date - datetime.timedelta(days=days)

        records = list(
            VitalSignRecord.objects
            .filter(patient=patient, record_date__gte=start_date)
            .order_by('record_date')
        )

        data_points = []
        labels = []

        INDICATOR_FIELDS = {
            'blood_pressure': ('systolic_bp', 'diastolic_bp'),
            'heart_rate': ('heart_rate',),
            'blood_glucose': ('blood_glucose',),
            'temperature': ('temperature',),
            'blood_oxygen': ('blood_oxygen',),
            'weight': ('weight',),
            'bmi': ('bmi',),
            'pain': ('pain_score',),
        }

        fields = INDICATOR_FIELDS.get(indicator, ())

        for record in records:
            date_str = record.record_date.strftime('%m-%d')
            labels.append(date_str)
            point = {'date': str(record.record_date), 'record_id': str(record.id)}
            for field in fields:
                val = getattr(record, field, None)
                point[field] = round(float(val), 1) if val is not None else None
            data_points.append(point)

        result = {
            'indicator': indicator,
            'days': days,
            'labels': labels,
            'data_points': data_points,
        }
        for field in fields:
            key = field.replace('_', '')
            result[key] = [p.get(field) for p in data_points]

        return result


class LabTestService:
    """检验报告服务"""

    def create_lab_test(self, patient: PatientProfile, data: Dict[str, Any]) -> LabTestRecord:
        if not data.get('lab_type'):
            raise ValueError('检验类型不能为空')
        if not data.get('test_date'):
            data['test_date'] = datetime.date.today()

        return LabTestRecord.objects.create(patient=patient, **data)

    def get_lab_tests_by_patient(self, patient: PatientProfile) -> List[LabTestRecord]:
        return list(
            LabTestRecord.objects
            .filter(patient=patient)
            .order_by('-test_date', '-created_at')
        )

    def delete_lab_test(self, record_id: int, user) -> bool:
        try:
            record = LabTestRecord.objects.get(id=record_id)
        except LabTestRecord.DoesNotExist:
            raise PermissionDenied('检验报告不存在')
        if record.patient.user_id != user.id:
            raise PermissionDenied('无权删除他人的检验报告')
        record.delete()
        return True


class ImagingService:
    """影像检查服务"""

    def create_imaging(self, patient: PatientProfile, data: Dict[str, Any]) -> ImagingRecord:
        if not data.get('imaging_type'):
            raise ValueError('检查类型不能为空')
        if not data.get('exam_date'):
            data['exam_date'] = datetime.date.today()

        return ImagingRecord.objects.create(patient=patient, **data)

    def get_imaging_by_patient(self, patient: PatientProfile) -> List[ImagingRecord]:
        return list(
            ImagingRecord.objects
            .filter(patient=patient)
            .order_by('-exam_date', '-created_at')
        )

    def delete_imaging(self, record_id: int, user) -> bool:
        try:
            record = ImagingRecord.objects.get(id=record_id)
        except ImagingRecord.DoesNotExist:
            raise PermissionDenied('影像检查不存在')
        if record.patient.user_id != user.id:
            raise PermissionDenied('无权删除他人的影像检查记录')
        record.delete()
        return True


class MedicalRecordAggregator:
    """医疗记录聚合服务 - 合并三种类型的记录"""

    def get_all_records(self, patient: PatientProfile, limit: int = 50) -> List[dict]:
        """获取所有医疗记录（合并生命体征、检验报告、影像检查）

        Args:
            patient: 患者档案
            limit: 返回记录上限，默认 50 条
        """
        records = []

        for record in VitalSignRecord.objects.filter(patient=patient).order_by('-record_date', '-record_time', '-created_at')[:limit]:
            records.append({
                'type': 'vital_sign',
                'type_display': '生命体征',
                'date': record.record_date,
                'source': record.source,
                'record': record,
                'summary': self._vital_summary(record),
            })

        for record in LabTestRecord.objects.filter(patient=patient).order_by('-test_date', '-created_at')[:limit]:
            records.append({
                'type': 'lab_test',
                'type_display': record.get_lab_type_display(),
                'date': record.test_date,
                'source': record.source,
                'record': record,
                'summary': ', '.join([f"{k}: {v}" for k, v in list(record.data.items())[:3]]),
            })

        for record in ImagingRecord.objects.filter(patient=patient).order_by('-exam_date', '-created_at')[:limit]:
            records.append({
                'type': 'imaging',
                'type_display': record.get_imaging_type_display(),
                'date': record.exam_date,
                'source': record.source,
                'record': record,
                'summary': record.conclusion[:50] if record.conclusion else '',
            })

        records.sort(key=lambda x: x['date'], reverse=True)
        return records[:limit]

    def _vital_summary(self, record: VitalSignRecord) -> str:
        """生成生命体征摘要"""
        parts = []
        if record.systolic_bp and record.diastolic_bp:
            parts.append(f"血压 {record.systolic_bp}/{record.diastolic_bp}")
        if record.heart_rate:
            parts.append(f"心率 {record.heart_rate}")
        if record.temperature:
            parts.append(f"体温 {record.temperature}°C")
        return ', '.join(parts)


class MedicationService:
    """用药记录服务（V1：手动录入）"""

    def create_medication(self, patient: PatientProfile, data: Dict[str, Any]) -> MedicationRecord:
        if not data.get('medication_name'):
            raise ValueError('药品名称不能为空')

        return MedicationRecord.objects.create(
            patient=patient,
            medication_name=data.get('medication_name', ''),
            dosage=data.get('dosage', ''),
            frequency=data.get('frequency', ''),
            route=data.get('route', ''),
            start_date=data.get('start_date', datetime.date.today()),
            end_date=data.get('end_date'),
            status=data.get('status', 'active'),
            prescriber=data.get('prescriber', ''),
            notes=data.get('notes', ''),
        )

    def get_current_medications(self, patient: PatientProfile) -> List[MedicationRecord]:
        return list(
            MedicationRecord.objects
            .filter(patient=patient, status='active')
            .order_by('-start_date', '-created_at')
        )

    def get_medication_history(self, patient: PatientProfile) -> List[MedicationRecord]:
        return list(
            MedicationRecord.objects
            .filter(patient=patient)
            .order_by('-start_date', '-created_at')
        )

    def stop_medication(self, record_id: int, user, end_date=None) -> MedicationRecord:
        try:
            record = MedicationRecord.objects.get(id=record_id)
        except MedicationRecord.DoesNotExist:
            raise PermissionDenied('用药记录不存在')
        if record.patient.user_id != user.id:
            raise PermissionDenied('无权操作他人的用药记录')
        record.stop(end_date=end_date)
        return record

    def resume_medication(self, record_id: int, user) -> MedicationRecord:
        try:
            record = MedicationRecord.objects.get(id=record_id)
        except MedicationRecord.DoesNotExist:
            raise PermissionDenied('用药记录不存在')
        if record.patient.user_id != user.id:
            raise PermissionDenied('无权操作他人的用药记录')
        try:
            record.resume()
        except ValidationError as e:
            raise PermissionDenied(str(e.message) if hasattr(e, 'message') else str(e))
        return record

    def delete_medication(self, record_id: int, user) -> bool:
        try:
            record = MedicationRecord.objects.get(id=record_id)
        except MedicationRecord.DoesNotExist:
            raise PermissionDenied('用药记录不存在')
        if record.patient.user_id != user.id:
            raise PermissionDenied('无权删除他人的用药记录')
        record.delete()
        return True
