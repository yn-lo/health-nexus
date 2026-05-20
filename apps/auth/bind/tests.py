"""Tests for binding service and views."""
from django.test import TestCase, Client
from apps.auth.bind.services import BindingService
from apps.auth.bind.models import PatientDoctorBinding
from apps.auth.models import UserProfile


class BindingServiceTest(TestCase):

    def setUp(self):
        self.doctor = UserProfile.objects.create_user(
            username='doctor_test',
            password='pass123',
            role=UserProfile.Role.DOCTOR,
        )
        self.patient = UserProfile.objects.create_user(
            username='patient_test',
            password='pass123',
            role=UserProfile.Role.PATIENT,
        )
        self.service = BindingService()

    def test_doctor_initiates_binding_patient_id_is_set(self):
        """When doctor initiates binding, patient_id should be target_user_id (the patient)."""
        success, msg = self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        self.assertTrue(success, msg)

        binding = PatientDoctorBinding.objects.get(
            doctor_id=self.doctor.id,
            patient_id=self.patient.id,
        )
        self.assertEqual(binding.initiator, 'DOCTOR')
        self.assertEqual(binding.status, PatientDoctorBinding.Status.PENDING)

    def test_patient_initiates_binding_patient_id_is_set(self):
        """When patient initiates binding, patient_id should be initiator_user_id."""
        success, msg = self.service.create_binding_request(
            target_user_id=self.doctor.id,
            initiator_user_id=self.patient.id,
            initiator_type='PATIENT',
        )
        self.assertTrue(success, msg)

        binding = PatientDoctorBinding.objects.get(
            doctor_id=self.doctor.id,
            patient_id=self.patient.id,
        )
        self.assertEqual(binding.initiator, 'PATIENT')

    def test_duplicate_doctor_patient_binding_rejected(self):
        """Duplicate binding between same doctor and patient should be rejected."""
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        success, msg = self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        self.assertFalse(success)
        self.assertIn('已存在', msg)

    def test_doctor_cannot_bind_doctor(self):
        """Doctor cannot bind to another doctor."""
        doctor2 = self.doctor.__class__.objects.create_user(
            username='doctor2_test',
            password='pass123',
            role=self.doctor.__class__.Role.DOCTOR,
        )
        success, msg = self.service.create_binding_request(
            target_user_id=doctor2.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        self.assertFalse(success)

    def test_patient_cannot_bind_patient(self):
        """Patient cannot bind to another patient."""
        patient2 = self.patient.__class__.objects.create_user(
            username='patient2_test',
            password='pass123',
            role=self.patient.__class__.Role.PATIENT,
        )
        success, msg = self.service.create_binding_request(
            target_user_id=patient2.id,
            initiator_user_id=self.patient.id,
            initiator_type='PATIENT',
        )
        self.assertFalse(success)


class BindingConfirmRejectTest(TestCase):
    """Test confirm and reject binding with status lifecycle."""

    def setUp(self):
        self.doctor = UserProfile.objects.create_user(
            username='doctor_cr',
            password='pass123',
            role=UserProfile.Role.DOCTOR,
        )
        self.patient = UserProfile.objects.create_user(
            username='patient_cr',
            password='pass123',
            role=UserProfile.Role.PATIENT,
        )
        self.service = BindingService()

    def test_confirm_binding_changes_status_to_confirmed(self):
        """Confirming a pending binding should set status to CONFIRMED and confirmed_at."""
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        binding = PatientDoctorBinding.objects.get(
            doctor_id=self.doctor.id,
            patient_id=self.patient.id,
        )
        success, msg = self.service.confirm_binding(binding.id, self.patient.id)
        self.assertTrue(success)
        binding.refresh_from_db()
        self.assertEqual(binding.status, PatientDoctorBinding.Status.CONFIRMED)
        self.assertIsNotNone(binding.confirmed_at)

    def test_reject_binding_changes_status_to_rejected(self):
        """Rejecting a pending binding should set status to REJECTED."""
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        binding = PatientDoctorBinding.objects.get(
            doctor_id=self.doctor.id,
            patient_id=self.patient.id,
        )
        success, msg = self.service.reject_binding(binding.id, self.patient.id)
        self.assertTrue(success)
        binding.refresh_from_db()
        self.assertEqual(binding.status, PatientDoctorBinding.Status.REJECTED)

    def test_confirmed_binding_cannot_be_confirmed_again(self):
        """Confirming an already confirmed binding should fail."""
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        binding = PatientDoctorBinding.objects.get(
            doctor_id=self.doctor.id,
            patient_id=self.patient.id,
        )
        self.service.confirm_binding(binding.id, self.patient.id)
        success, msg = self.service.confirm_binding(binding.id, self.patient.id)
        self.assertFalse(success)

    def test_non_participant_cannot_confirm_binding(self):
        """A user not involved in the binding cannot confirm it."""
        other_patient = UserProfile.objects.create_user(
            username='other_patient',
            password='pass123',
            role=UserProfile.Role.PATIENT,
        )
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )
        binding = PatientDoctorBinding.objects.get(
            doctor_id=self.doctor.id,
            patient_id=self.patient.id,
        )
        success, msg = self.service.confirm_binding(binding.id, other_patient.id)
        self.assertFalse(success)


class BindRequestListViewTest(TestCase):
    """Test list_bind_requests view shows correct other_user."""

    def setUp(self):
        self.doctor = UserProfile.objects.create_user(
            username='doctor_bind',
            password='pass123',
            role=UserProfile.Role.DOCTOR,
        )
        self.patient = UserProfile.objects.create_user(
            username='patient_bind',
            password='pass123',
            role=UserProfile.Role.PATIENT,
        )
        self.service = BindingService()

    def _login(self, user):
        client = Client()
        client.login(username=user.username, password='pass123')
        return client

    def test_doctor_views_own_request_shows_patient(self):
        """Doctor initiated binding, should see patient in request list."""
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )

        client = self._login(self.doctor)
        response = client.get('/accounts/bind/requests/')
        self.assertEqual(response.status_code, 200)
        requests = response.context['requests']
        self.assertEqual(len(requests), 1)
        self.assertEqual(requests[0]['other_user'], self.patient)

    def test_patient_views_doctor_initiated_shows_doctor(self):
        """Doctor initiated binding, patient should see doctor in request list."""
        self.service.create_binding_request(
            target_user_id=self.patient.id,
            initiator_user_id=self.doctor.id,
            initiator_type='DOCTOR',
        )

        client = self._login(self.patient)
        response = client.get('/accounts/bind/requests/')
        self.assertEqual(response.status_code, 200)
        requests = response.context['requests']
        self.assertEqual(len(requests), 1)
        self.assertEqual(requests[0]['other_user'], self.doctor)

    def test_patient_views_own_request_shows_doctor(self):
        """Patient initiated binding, should see doctor in request list."""
        self.service.create_binding_request(
            target_user_id=self.doctor.id,
            initiator_user_id=self.patient.id,
            initiator_type='PATIENT',
        )

        client = self._login(self.patient)
        response = client.get('/accounts/bind/requests/')
        self.assertEqual(response.status_code, 200)
        requests = response.context['requests']
        self.assertEqual(len(requests), 1)
        self.assertEqual(requests[0]['other_user'], self.doctor)

    def test_doctor_views_patient_initiated_shows_patient(self):
        """Patient initiated binding, doctor should see patient in request list."""
        self.service.create_binding_request(
            target_user_id=self.doctor.id,
            initiator_user_id=self.patient.id,
            initiator_type='PATIENT',
        )

        client = self._login(self.doctor)
        response = client.get('/accounts/bind/requests/')
        self.assertEqual(response.status_code, 200)
        requests = response.context['requests']
        self.assertEqual(len(requests), 1)
        self.assertEqual(requests[0]['other_user'], self.patient)
