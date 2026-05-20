"""
服务容器 - 依赖注入的核心实现

替代全局可变状态，提供类型安全的服务解析。
"""

import logging
from typing import Optional

from apps.interfaces import (
    IDepartmentService,
    IUserService,
    ILoginSecurityService,
    IPatientCRUDService,
    IPatientContextService,
    IKnowledgeSearchService,
    IWikiService,
    IRAGService,
    IChatManagementService,
    IStatsService,
    IUserSettingsService,
    IChatSendService,
    IFeedbackService,
    IVitalSignService,
    ILabTestService,
    IImagingService,
    IMedicationService,
    IMedicalRecordAggregator,
    IArticleReferenceService,
    IAuditLogService,
    ISMSService,
    IBindingService,
    IBulkImportService,
    IStaffDashboardService,
)

logger = logging.getLogger(__name__)


class ServiceContainer:
    """线程安全的服务容器（Django 单线程模型下足够）"""

    def __init__(self):
        self._department_service: Optional[IDepartmentService] = None
        self._user_service: Optional[IUserService] = None
        self._login_security_service: Optional[ILoginSecurityService] = None
        self._patient_crud_service: Optional[IPatientCRUDService] = None
        self._patient_context_service: Optional[IPatientContextService] = None
        self._knowledge_search_service: Optional[IKnowledgeSearchService] = None
        self._wiki_service: Optional[IWikiService] = None
        self._rag_service: Optional[IRAGService] = None
        self._chat_management_service: Optional[IChatManagementService] = None
        self._stats_service: Optional[IStatsService] = None
        self._user_settings_service: Optional[IUserSettingsService] = None
        self._chat_send_service: Optional[IChatSendService] = None
        self._feedback_service: Optional[IFeedbackService] = None
        self._vital_sign_service: Optional[IVitalSignService] = None
        self._lab_test_service: Optional[ILabTestService] = None
        self._imaging_service: Optional[IImagingService] = None
        self._medication_service: Optional[IMedicationService] = None
        self._medical_record_aggregator: Optional[IMedicalRecordAggregator] = None
        self._article_reference_service: Optional[IArticleReferenceService] = None
        self._audit_log_service: Optional[IAuditLogService] = None
        self._sms_service: Optional[ISMSService] = None
        self._binding_service: Optional[IBindingService] = None
        self._bulk_import_service: Optional[IBulkImportService] = None
        self._staff_dashboard_service: Optional[IStaffDashboardService] = None

    def register(self, **services) -> None:
        for name, svc in services.items():
            attr_name = f'_{name}'
            if hasattr(self, attr_name):
                setattr(self, attr_name, svc)
            else:
                logger.warning("Unknown service registration: %s", name)

    @property
    def department_service(self) -> IDepartmentService:
        if self._department_service is None:
            raise RuntimeError("DepartmentService not registered")
        return self._department_service

    @property
    def user_service(self) -> IUserService:
        if self._user_service is None:
            raise RuntimeError("UserService not registered")
        return self._user_service

    @property
    def patient_crud_service(self) -> IPatientCRUDService:
        if self._patient_crud_service is None:
            raise RuntimeError("PatientCRUDService not registered")
        return self._patient_crud_service

    @property
    def patient_context_service(self) -> IPatientContextService:
        if self._patient_context_service is None:
            raise RuntimeError("PatientContextService not registered")
        return self._patient_context_service

    @property
    def knowledge_search_service(self) -> IKnowledgeSearchService:
        if self._knowledge_search_service is None:
            raise RuntimeError("KnowledgeSearchService not registered")
        return self._knowledge_search_service

    @property
    def wiki_service(self) -> IWikiService:
        if self._wiki_service is None:
            raise RuntimeError("WikiService not registered")
        return self._wiki_service

    @property
    def rag_service(self) -> IRAGService:
        if self._rag_service is None:
            raise RuntimeError("RAGService not registered")
        return self._rag_service

    @property
    def chat_management_service(self) -> IChatManagementService:
        if self._chat_management_service is None:
            raise RuntimeError("ChatManagementService not registered")
        return self._chat_management_service

    @property
    def stats_service(self) -> IStatsService:
        if self._stats_service is None:
            raise RuntimeError("StatsService not registered")
        return self._stats_service

    @property
    def user_settings_service(self) -> IUserSettingsService:
        if self._user_settings_service is None:
            raise RuntimeError("UserSettingsService not registered")
        return self._user_settings_service

    @property
    def login_security_service(self) -> ILoginSecurityService:
        if self._login_security_service is None:
            raise RuntimeError("LoginSecurityService not registered")
        return self._login_security_service

    @property
    def chat_send_service(self) -> IChatSendService:
        if self._chat_send_service is None:
            raise RuntimeError("ChatSendService not registered")
        return self._chat_send_service

    @property
    def feedback_service(self) -> IFeedbackService:
        if self._feedback_service is None:
            raise RuntimeError("FeedbackService not registered")
        return self._feedback_service

    @property
    def vital_sign_service(self) -> IVitalSignService:
        if self._vital_sign_service is None:
            raise RuntimeError("VitalSignService not registered")
        return self._vital_sign_service

    @property
    def lab_test_service(self) -> ILabTestService:
        if self._lab_test_service is None:
            raise RuntimeError("LabTestService not registered")
        return self._lab_test_service

    @property
    def imaging_service(self) -> IImagingService:
        if self._imaging_service is None:
            raise RuntimeError("ImagingService not registered")
        return self._imaging_service

    @property
    def medication_service(self) -> IMedicationService:
        if self._medication_service is None:
            raise RuntimeError("MedicationService not registered")
        return self._medication_service

    @property
    def medical_record_aggregator(self) -> IMedicalRecordAggregator:
        if self._medical_record_aggregator is None:
            raise RuntimeError("MedicalRecordAggregator not registered")
        return self._medical_record_aggregator

    @property
    def article_reference_service(self) -> IArticleReferenceService:
        if self._article_reference_service is None:
            raise RuntimeError("ArticleReferenceService not registered")
        return self._article_reference_service

    @property
    def audit_log_service(self) -> IAuditLogService:
        if self._audit_log_service is None:
            raise RuntimeError("AuditLogService not registered")
        return self._audit_log_service

    @property
    def sms_service(self) -> ISMSService:
        if self._sms_service is None:
            raise RuntimeError("SMSService not registered")
        return self._sms_service

    @property
    def binding_service(self) -> IBindingService:
        if self._binding_service is None:
            raise RuntimeError("BindingService not registered")
        return self._binding_service

    @property
    def bulk_import_service(self) -> IBulkImportService:
        if self._bulk_import_service is None:
            raise RuntimeError("BulkImportService not registered")
        return self._bulk_import_service

    @property
    def staff_dashboard_service(self) -> IStaffDashboardService:
        if self._staff_dashboard_service is None:
            raise RuntimeError("StaffDashboardService not registered")
        return self._staff_dashboard_service


container = ServiceContainer()


def initialize_real_services() -> None:
    """实例化所有服务并注册到容器。"""
    from apps.base.services import DepartmentService
    from apps.auth.services.user_service import UserService
    from apps.auth.services.user_settings_service import UserSettingsService
    from apps.auth.services.login_security_service import LoginSecurityService
    from apps.auth.services.sms_service import SMSService
    from apps.auth.bind.services import BindingService
    from apps.auth.services.bulk_import_service import BulkImportService
    from apps.auth.services.staff_dashboard_service import StaffDashboardService
    from apps.care.services import (
        PatientService, VitalSignService, LabTestService,
        ImagingService, MedicationService, MedicalRecordAggregator,
    )
    from apps.wiki.knowledge_search_service import KnowledgeSearchService
    from apps.wiki.services import WikiService
    from apps.chat.rag import RAGService
    from apps.chat.services import ConversationService, FeedbackService
    from apps.chat.services.chat_send_service import ChatSendService
    from apps.stats.services.stats_service import StatsService
    from apps.wiki.reference.services import ArticleReferenceService
    from apps.base.services import AuditLogService

    patient_svc = PatientService()

    container.register(
        department_service=DepartmentService(),
        user_service=UserService(),
        login_security_service=LoginSecurityService(),
        patient_crud_service=patient_svc,
        patient_context_service=patient_svc,
        knowledge_search_service=KnowledgeSearchService(),
        wiki_service=WikiService(),
        rag_service=RAGService(),
        chat_management_service=ConversationService(),
        stats_service=StatsService(),
        user_settings_service=UserSettingsService(),
        chat_send_service=ChatSendService(),
        feedback_service=FeedbackService(),
        vital_sign_service=VitalSignService(),
        lab_test_service=LabTestService(),
        imaging_service=ImagingService(),
        medication_service=MedicationService(),
        medical_record_aggregator=MedicalRecordAggregator(),
        article_reference_service=ArticleReferenceService(),
        audit_log_service=AuditLogService(),
        sms_service=SMSService(),
        binding_service=BindingService(),
        bulk_import_service=BulkImportService(),
        staff_dashboard_service=StaffDashboardService(),
    )
