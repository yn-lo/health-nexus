import datetime
import logging
import os
from django.db.models.signals import post_migrate
from django.dispatch import receiver
from django.contrib.auth import get_user_model
from apps.base.models import Department
from apps.auth.models import UserProfile

logger = logging.getLogger(__name__)


@receiver(post_migrate)
def init_app_data(sender, **kwargs):
    if sender.name not in ['apps.base', 'apps.auth', 'apps.chat']:
        return

    User = get_user_model()

    superuser_username = os.environ.get('SUPERUSER_USERNAME', 'admin')
    superuser_email = os.environ.get('SUPERUSER_EMAIL', 'admin@example.com')
    superuser_password = os.environ.get('SUPERUSER_PASSWORD', 'admin')

    if not User.objects.filter(username=superuser_username).exists():
        logger.info("Creating default superuser: %s", superuser_username)
        User.objects.create_superuser(superuser_username, superuser_email, superuser_password)

    create_test_data = os.environ.get('CREATE_TEST_DATA', 'False').lower() == 'true'
    if create_test_data and Department.objects.count() == 0:
        logger.info("Initializing test data for development...")

        cardiology = Department.objects.create(
            name="心内科",
            description="心血管疾病诊疗中心",
            config={"welcome_msg": "您好，我是心内科AI助手，请问有什么可以帮您？"}
        )
        endocrinology = Department.objects.create(
            name="内分泌科",
            description="糖尿病与代谢病中心"
        )
        public_dept = Department.objects.create(
            name="公共卫生科",
            description="公共健康知识",
            is_public=True
        )

        User.objects.create_user(
            username='doctor_zhang',
            password='password123',
            role=UserProfile.Role.DOCTOR,
            first_name="张",
            last_name="医生"
        )

        patient_user = User.objects.create_user(
            username='patient_li',
            password='password123',
            role=UserProfile.Role.PATIENT,
            first_name="李",
            last_name="大爷",
            agreed_terms=True,
        )

        from apps.care.models import PatientProfile, VitalSignRecord
        profile, _ = PatientProfile.objects.get_or_create(
            user=patient_user,
            defaults={
                'name': "李建国",
                'birth_date': "1955-05-20",
                'gender': "M",
                'medical_history_summary': "高血压10年，冠心病史",
            }
        )
        profile.departments.add(cardiology)
        VitalSignRecord.objects.create(
            patient=profile,
            record_date=datetime.date.today(),
            systolic_bp=150,
            diastolic_bp=95,
            heart_rate=78,
        )

        logger.info("Test data initialized successfully.")

