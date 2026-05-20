from django.db import models
from django.conf import settings
from django.utils import timezone


class PatientDoctorBinding(models.Model):
    class Status(models.TextChoices):
        PENDING = 'PENDING', '待确认'
        CONFIRMED = 'CONFIRMED', '已绑定'
        REJECTED = 'REJECTED', '已拒绝'
        REVOKED = 'REVOKED', '已解绑'

    patient = models.ForeignKey(
        settings.AUTH_USER_MODEL,
        on_delete=models.CASCADE,
        related_name='doctor_bindings',
        limit_choices_to={'role': 'PATIENT'},
        null=True,
        blank=True
    )
    doctor = models.ForeignKey(
        settings.AUTH_USER_MODEL,
        on_delete=models.CASCADE,
        related_name='patient_bindings',
        limit_choices_to={'role__in': ['DOCTOR', 'NURSE']}
    )
    initiator = models.CharField(
        max_length=10,
        choices=[('PATIENT', '患者'), ('DOCTOR', '医护')],
        default='DOCTOR',
        help_text="绑定请求发起方"
    )
    status = models.CharField(
        max_length=20,
        choices=Status.choices,
        default=Status.PENDING,
        help_text="绑定状态"
    )
    requested_at = models.DateTimeField(auto_now_add=True)
    confirmed_at = models.DateTimeField(null=True, blank=True)
    revoked_at = models.DateTimeField(null=True, blank=True)

    class Meta:
        unique_together = ['patient', 'doctor']
        verbose_name = "医患绑定"
        verbose_name_plural = "医患绑定"
        ordering = ['-requested_at']

    def __str__(self):
        patient_str = self.patient.username if self.patient else "未绑定"
        return f"{self.doctor.username} -> {patient_str} ({self.get_status_display()})"

    def confirm(self):
        self.status = self.Status.CONFIRMED
        self.confirmed_at = timezone.now()
        self.save(update_fields=['status', 'confirmed_at'])

    def reject(self):
        self.status = self.Status.REJECTED
        self.save(update_fields=['status'])

    def revoke(self):
        self.status = self.Status.REVOKED
        self.revoked_at = timezone.now()
        self.save(update_fields=['status', 'revoked_at'])
