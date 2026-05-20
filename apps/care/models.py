import uuid
from django.db import models
from django.utils.translation import gettext_lazy as _
from django.core.exceptions import ValidationError
from encrypted_model_fields.fields import EncryptedCharField
from apps.base.models import TimeStampedModel, Department
from apps.auth.models import UserProfile
import datetime


class PatientProfile(TimeStampedModel):
    """患者档案 - 关联多个科室"""
    user = models.OneToOneField(UserProfile, on_delete=models.CASCADE, related_name='patient_profile', verbose_name=_("关联账号"))
    
    # 核心业务：患者属于多个科室
    departments = models.ManyToManyField(
        Department, 
        related_name='patients', 
        verbose_name=_("就诊科室"),
        help_text=_("AI回答时会优先检索这些科室的知识库")
    )
    
    # 基础信息 (部分加密)
    name = models.CharField(_("真实姓名"), max_length=50)
    id_number = EncryptedCharField(max_length=20, blank=True, null=True, verbose_name=_("身份证号 (加密)"))
    birth_date = models.DateField(_("出生日期"), null=True, blank=True)
    gender = models.CharField(_("性别"), max_length=10, choices=[('M', '男'), ('F', '女'), ('O', '其他')], blank=True)
    
    # 长期不变化的档案属性
    height = models.DecimalField(_("身高 (cm)"), max_digits=5, decimal_places=1, null=True, blank=True)
    weight = models.DecimalField(_("体重 (kg)"), max_digits=5, decimal_places=1, null=True, blank=True)
    education = models.CharField(_("文化程度"), max_length=20, choices=[
        ('primary', _('小学')),
        ('junior', _('初中')),
        ('senior', _('高中/中专')),
        ('college', _('大专')),
        ('bachelor', _('本科')),
        ('master', _('硕士')),
        ('doctor', _('博士')),
        ('other', _('其他')),
    ], blank=True, default='')
    marital_status = models.CharField(_("婚姻状况"), max_length=10, choices=[
        ('single', _('未婚')),
        ('married', _('已婚')),
        ('divorced', _('离异')),
        ('widowed', _('丧偶')),
    ], blank=True, default='')
    occupation = models.CharField(_("职业"), max_length=50, blank=True, default='')
    blood_type = models.CharField(_("血型"), max_length=10, choices=[
        ('A', _('A型')),
        ('B', _('B型')),
        ('AB', _('AB型')),
        ('O', _('O型')),
        ('unknown', _('未知')),
    ], blank=True, default='')
    phone = models.CharField(_("联系电话"), max_length=20, blank=True, default='')
    address = models.CharField(_("家庭住址"), max_length=200, blank=True, default='')
    emergency_contact = models.CharField(_("紧急联系人"), max_length=50, blank=True, default='')
    emergency_phone = models.CharField(_("紧急联系电话"), max_length=20, blank=True, default='')
    
    # 核心健康数据 (用于 RAG 上下文)
    # 既往病史/过敏史使用加密存储，保护敏感健康数据
    medical_history_summary = EncryptedCharField(
        _("既往病史摘要"),
        max_length=5000,
        blank=True,
        null=True,
        help_text=_("如：高血压3年，糖尿病2年")
    )
    allergies_summary = EncryptedCharField(
        _("过敏史摘要"),
        max_length=2000,
        blank=True,
        null=True
    )
    
    # 结构化数据 (如最近一次关键指标 - 查询 VitalSignRecord 最新记录)
    # 使用属性访问，非数据库字段

    class Meta:
        verbose_name = _("患者档案")
        verbose_name_plural = _("患者档案")

    def __str__(self):
        return f"{self.name} ({self.user.username})"

    @property
    def age(self):
        if self.birth_date:
            today = datetime.date.today()
            return today.year - self.birth_date.year - ((today.month, today.day) < (self.birth_date.month, self.birth_date.day))
        return 0

    @property
    def latest_vitals(self):
        """获取最新的生命体征数据（返回 dict 格式，保持向后兼容）"""
        try:
            record = self.vital_signs.latest('record_date', 'record_time', 'created_at')
            return {
                'bp_high': record.systolic_bp,
                'bp_low': record.diastolic_bp,
                'heart_rate': record.heart_rate,
                'pulse': record.pulse,
                'temperature': float(record.temperature) if record.temperature else None,
                'blood_glucose': float(record.blood_glucose) if record.blood_glucose else None,
                'blood_oxygen': record.blood_oxygen,
                'weight': float(record.weight) if record.weight else None,
                'height': float(record.height) if record.height else None,
                'bmi': float(record.bmi) if record.bmi else None,
                'pain_score': record.pain_score,
            }
        except VitalSignRecord.DoesNotExist:
            return {}


class VitalSignRecord(TimeStampedModel):
    """生命体征记录 - 强类型字段，支持范围校验"""
    MEASUREMENT_CONTEXT_CHOICES = [
        ('fasting', _('空腹')),
        ('post_meal', _('餐后')),
        ('resting', _('静息')),
        ('post_exercise', _('运动后')),
        ('other', _('其他')),
    ]

    id = models.UUIDField(_("记录ID"), primary_key=True, default=uuid.uuid4, editable=False)
    patient = models.ForeignKey(PatientProfile, on_delete=models.CASCADE, related_name='vital_signs', verbose_name=_("所属患者"))
    record_date = models.DateField(_("测量日期"))
    record_time = models.TimeField(_("测量时间"), null=True, blank=True)
    source = models.CharField(_("来源"), max_length=100, blank=True, help_text=_("设备名称/医院"))
    
    # 血压
    systolic_bp = models.IntegerField(_("收缩压 (mmHg)"), null=True, blank=True, help_text=_("正常范围: 90-140"))
    diastolic_bp = models.IntegerField(_("舒张压 (mmHg)"), null=True, blank=True, help_text=_("正常范围: 60-90"))
    
    # 心血管
    heart_rate = models.IntegerField(_("心率 (次/分)"), null=True, blank=True, help_text=_("正常范围: 60-100"))
    pulse = models.IntegerField(_("脉搏 (次/分)"), null=True, blank=True, help_text=_("正常范围: 60-100"))
    
    # 其他体征
    temperature = models.DecimalField(_("体温 (°C)"), max_digits=4, decimal_places=1, null=True, blank=True, help_text=_("正常范围: 36.1-37.2"))
    respiratory_rate = models.IntegerField(_("呼吸频率 (次/分)"), null=True, blank=True, help_text=_("正常范围: 12-20"))
    blood_glucose = models.DecimalField(_("血糖 (mmol/L)"), max_digits=4, decimal_places=1, null=True, blank=True, help_text=_("正常范围: 3.9-6.1 (空腹)"))
    blood_oxygen = models.IntegerField(_("血氧饱和度 (%)"), null=True, blank=True, help_text=_("正常范围: 95-100"))
    
    # 体格测量
    weight = models.DecimalField(_("体重 (kg)"), max_digits=5, decimal_places=1, null=True, blank=True)
    height = models.DecimalField(_("身高 (cm)"), max_digits=5, decimal_places=1, null=True, blank=True)
    bmi = models.DecimalField(_("BMI"), max_digits=4, decimal_places=1, null=True, blank=True, help_text=_("可自动计算"))
    
    # 疼痛评估
    pain_score = models.IntegerField(_("疼痛得分 (NRS 0-10)"), null=True, blank=True, help_text=_("0=无痛, 10=剧痛"))
    
    # 上下文
    measurement_context = models.CharField(_("测量场景"), max_length=50, choices=MEASUREMENT_CONTEXT_CHOICES, blank=True)
    notes = models.TextField(_("备注"), blank=True)

    class Meta:
        verbose_name = _("生命体征记录")
        verbose_name_plural = _("生命体征记录")
        ordering = ['-record_date', '-record_time', '-created_at']

    def __str__(self):
        return f"{self.patient.name} - 生命体征 ({self.record_date})"

    def save(self, *args, **kwargs):
        # 自动计算 BMI
        if self.weight and self.height and not self.bmi:
            height_m = self.height / 100
            if height_m > 0:
                self.bmi = round(self.weight / (height_m ** 2), 1)
        super().save(*args, **kwargs)


class LabTestRecord(TimeStampedModel):
    """检验报告 - 按类型模板化录入"""
    LAB_TYPE_CHOICES = [
        ('blood_routine', _('血常规')),
        ('biochemistry', _('生化全套')),
        ('urine_routine', _('尿常规')),
        ('thyroid_function', _('甲状腺功能')),
        ('hba1c', _('糖化血红蛋白')),
        ('other', _('其他')),
    ]

    id = models.UUIDField(_("记录ID"), primary_key=True, default=uuid.uuid4, editable=False)
    patient = models.ForeignKey(PatientProfile, on_delete=models.CASCADE, related_name='lab_tests', verbose_name=_("所属患者"))
    test_date = models.DateField(_("检查日期"))
    source = models.CharField(_("医院名称"), max_length=100, blank=True)
    lab_type = models.CharField(_("检验类型"), max_length=30, choices=LAB_TYPE_CHOICES)
    data = models.JSONField(_("检验指标"), default=dict, blank=True, help_text=_("如 {'WBC': 6.5, 'HGB': 145, 'PLT': 200}"))
    notes = models.TextField(_("备注"), blank=True)

    class Meta:
        verbose_name = _("检验报告")
        verbose_name_plural = _("检验报告")
        ordering = ['-test_date', '-created_at']

    def __str__(self):
        return f"{self.patient.name} - {self.get_lab_type_display()} ({self.test_date})"


class ImagingRecord(TimeStampedModel):
    """影像检查 - 所见和结论"""
    IMAGING_TYPE_CHOICES = [
        ('ct', _('CT')),
        ('mri', _('MRI')),
        ('xray', _('X光')),
        ('ultrasound', _('超声')),
        ('ecg', _('心电图')),
        ('other', _('其他')),
    ]

    id = models.UUIDField(_("记录ID"), primary_key=True, default=uuid.uuid4, editable=False)
    patient = models.ForeignKey(PatientProfile, on_delete=models.CASCADE, related_name='imaging_records', verbose_name=_("所属患者"))
    exam_date = models.DateField(_("检查日期"))
    source = models.CharField(_("医院名称"), max_length=100, blank=True)
    imaging_type = models.CharField(_("检查类型"), max_length=20, choices=IMAGING_TYPE_CHOICES)
    body_part = models.CharField(_("检查部位"), max_length=100, blank=True)
    findings = models.TextField(_("检查所见"), blank=True, help_text=_("影像描述"))
    conclusion = models.TextField(_("诊断结论"), blank=True)
    notes = models.TextField(_("备注"), blank=True)

    class Meta:
        verbose_name = _("影像检查")
        verbose_name_plural = _("影像检查")
        ordering = ['-exam_date', '-created_at']

    def __str__(self):
        return f"{self.patient.name} - {self.get_imaging_type_display()} ({self.exam_date})"


class MedicationRecord(TimeStampedModel):
    """用药记录 - 时间段数据"""
    STATUS_CHOICES = [
        ('active', _('正在服用')),
        ('paused', _('暂停')),
        ('stopped', _('已停药')),
    ]
    ROUTE_CHOICES = [
        ('oral', _('口服')),
        ('iv', _('静脉注射')),
        ('sc', _('皮下注射')),
        ('im', _('肌肉注射')),
        ('topical', _('外用')),
        ('inhalation', _('吸入')),
        ('other', _('其他')),
    ]

    id = models.UUIDField(_("记录ID"), primary_key=True, default=uuid.uuid4, editable=False)
    patient = models.ForeignKey(PatientProfile, on_delete=models.CASCADE, related_name='medications', verbose_name=_("所属患者"))
    medication_name = models.CharField(_("药品名称"), max_length=200)
    dosage = models.CharField(_("剂量"), max_length=200, blank=True, help_text=_("如 500mg"))
    frequency = models.CharField(_("频次"), max_length=200, blank=True, help_text=_("如 每日2次"))
    route = models.CharField(_("给药途径"), max_length=20, choices=ROUTE_CHOICES, blank=True)
    start_date = models.DateField(_("开始用药日期"))
    end_date = models.DateField(_("停药日期"), null=True, blank=True)
    status = models.CharField(_("状态"), max_length=20, choices=STATUS_CHOICES, default='active')
    prescriber = models.CharField(_("开药医生/医院"), max_length=200, blank=True)
    notes = models.TextField(_("备注"), blank=True, help_text=_("副作用、注意事项等"))

    class Meta:
        verbose_name = _("用药记录")
        verbose_name_plural = _("用药记录")
        ordering = ['-start_date', '-created_at']

    def __str__(self):
        return f"{self.patient.name} - {self.medication_name} ({self.status})"

    def stop(self, end_date=None):
        """停药"""
        self.status = 'stopped'
        self.end_date = end_date or datetime.date.today()
        self.save()

    def resume(self):
        """恢复用药 - BR-CARE-10: 已停药记录不可直接恢复"""
        if self.status == 'stopped':
            raise ValidationError('已停药记录不可直接恢复，请重新开药')
        self.status = 'active'
        self.end_date = None
        self.save()
