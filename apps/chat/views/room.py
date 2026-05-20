import json
import logging
import markdown
from datetime import date
from urllib.parse import quote, unquote
from django.shortcuts import render, get_object_or_404, redirect
from django.http import HttpResponse, StreamingHttpResponse
from django.template.loader import render_to_string
from django.views.decorators.http import require_POST, require_GET
from django.urls import reverse
from django.core.cache import cache
from apps.base.models import Department
from apps.care.models import PatientProfile
from apps.chat.models import Conversation, Message
from apps.service_container import container
from apps.chat.services.hot_question_service import HotQuestionService
from apps.config.services.ratelimit_service import RateLimitConfigService
from apps.auth.rate_limit.strategies import FixedWindowRateLimitStrategy
from apps.chat.services.chat_response_strategies import (
    ChatResponseStrategy,
    SSEResponseStrategy,
    ChatErrorType,
)

logger = logging.getLogger(__name__)


def chat_home(request):
    conversations = []
    patient_dept_ids = []

    dept_service = container.department_service
    dept_tree = dept_service.get_department_tree()

    patient_dept_ids = []
    if request.user.is_authenticated:
        try:
            patient = request.user.patient_profile
            conversations = Conversation.objects.filter(patient=patient, is_archived=False).order_by('-created_at')[:20]
            patient_dept_ids = list(patient.departments.values_list('id', flat=True))
        except PatientProfile.DoesNotExist:
            conversations = []
    else:
        session_key = request.session.session_key
        if session_key:
            conversations = Conversation.objects.filter(
                patient__isnull=True, session_key=session_key, is_archived=False
            ).order_by('-updated_at')[:20]

    if request.user.is_authenticated and patient_dept_ids:
        hot_questions = HotQuestionService.get_hot_questions(patient_dept_ids)
    else:
        public_depts = dept_service.get_public_departments()
        public_dept_ids = [d.id for d in public_depts]
        hot_questions = HotQuestionService.get_hot_questions(public_dept_ids)

    public_dept_ids = list(Department.objects.filter(is_public=True).values_list('id', flat=True))

    return render(request, 'chat/chat_home.html', {
        'conversations': conversations,
        'hot_questions_json': json.dumps(hot_questions, ensure_ascii=False),
        'dept_tree': dept_tree,
        'patient_dept_ids': [str(i) for i in patient_dept_ids],
        'public_dept_ids': [str(i) for i in public_dept_ids],
    })


def chat_new(request):
    message = request.GET.get('message', '').strip()
    if not message:
        return redirect('chat:chat_home')

    url_dept_ids = request.GET.getlist('departments')
    from apps.chat.orchestrator import ChatSendOrchestrator
    orchestrator = ChatSendOrchestrator()

    patient = None
    if request.user.is_authenticated:
        try:
            patient = request.user.patient_profile
        except PatientProfile.DoesNotExist:
            return redirect('care:profile_detail')

        if not url_dept_ids:
            selected_dept_ids = [d.id for d in patient.departments.all()]
        else:
            selected_dept_ids = [int(d) for d in url_dept_ids]

        selected_departments = list(Department.objects.filter(id__in=selected_dept_ids))
        conversation = orchestrator.get_or_create_conversation(
            patient, None, message, selected_departments=selected_departments
        )
    else:
        if not url_dept_ids:
            return redirect('chat:chat_home')

        selected_dept_ids = [int(d) for d in url_dept_ids]
        selected_departments = list(Department.objects.filter(id__in=selected_dept_ids))
        session_key = None
        try:
            request.session.create()
            session_key = request.session.session_key
        except Exception:
            logger.warning("Failed to create session for anonymous user")

        conversation = orchestrator.get_or_create_conversation(
            None, None, message, session_key=session_key, selected_departments=selected_departments
        )

    encoded_message = quote(message)
    url = f'{reverse("chat:chat_conversation", kwargs={"conversation_id": conversation.id})}?initial_message={encoded_message}'

    for dept_id in selected_dept_ids:
        url += f'&departments={quote(str(dept_id))}'

    return redirect(url)


def chat_conversation(request, conversation_id):
    patient = None
    available_departments = []
    initial_message = request.GET.get('initial_message', '')
    conversations = []
    patient_dept_ids = []
    dept_tree = []
    current_conversation = None
    chat_messages = []

    # 从 URL 读取用户手动选择的科室（优先级最高）
    url_dept_ids = request.GET.getlist('departments')

    if request.user.is_authenticated:
        try:
            patient = request.user.patient_profile
            conversations = Conversation.objects.filter(patient=patient, is_archived=False).order_by('-updated_at')
            current_conversation = get_object_or_404(
                Conversation, id=conversation_id, patient=patient, is_archived=False
            )
            chat_messages = current_conversation.messages.all().prefetch_related('reference_chunks__article')

            patient_dept_ids = list(patient.departments.values_list('id', flat=True))
            public_dept_ids = Department.objects.filter(is_public=True).values_list('id', flat=True)
            dept_ids = set(patient_dept_ids) | set(public_dept_ids)
            available_departments = Department.objects.filter(id__in=dept_ids).order_by('-is_public', 'name')

            dept_service = container.department_service
            dept_tree = dept_service.get_department_tree()
        except PatientProfile.DoesNotExist:
            available_departments = Department.objects.filter(is_public=True).order_by('name')
    else:
        session_key = request.session.session_key
        if session_key:
            current_conversation = get_object_or_404(
                Conversation, id=conversation_id, patient__isnull=True, session_key=session_key, is_archived=False
            )
            chat_messages = current_conversation.messages.all().prefetch_related('reference_chunks__article')
            conversations = Conversation.objects.filter(
                patient__isnull=True, session_key=session_key, is_archived=False
            ).order_by('-updated_at')
        available_departments = Department.objects.filter(is_public=True).order_by('name')

    # 如果 URL 中有 departments 参数，使用它作为预选择的科室
    # 否则使用患者的默认科室
    if url_dept_ids:
        selected_dept_ids = [str(d) for d in url_dept_ids]
    else:
        selected_dept_ids = [str(i) for i in patient_dept_ids]

    if initial_message:
        initial_message = unquote(initial_message)

    public_dept_ids = list(Department.objects.filter(is_public=True).values_list('id', flat=True))

    return render(request, 'chat/chat_conversation.html', {
        'patient': patient,
        'conversation': current_conversation,
        'conversations': conversations,
        'chat_messages': chat_messages,
        'available_departments': available_departments,
        'initial_message': initial_message,
        'dept_tree': dept_tree,
        'patient_dept_ids': selected_dept_ids,
        'public_dept_ids': [str(i) for i in public_dept_ids],
    })


@require_POST
def conversation_delete(request, conversation_id):
    if not request.user.is_authenticated:
        return HttpResponse('')

    try:
        patient = request.user.patient_profile
    except PatientProfile.DoesNotExist:
        return HttpResponse('')

    conversation = get_object_or_404(Conversation, id=conversation_id, patient=patient)
    conversation.delete()

    conversations = Conversation.objects.filter(patient=patient, is_archived=False).order_by('-updated_at')
    return render(request, 'chat/partials/conversation_list.html', {
        'conversations': conversations,
    })


@require_POST
def conversation_rename(request, conversation_id):
    if not request.user.is_authenticated:
        return HttpResponse('')

    try:
        patient = request.user.patient_profile
    except PatientProfile.DoesNotExist:
        return HttpResponse('')

    conversation = get_object_or_404(Conversation, id=conversation_id, patient=patient)
    new_title = request.POST.get('title', '').strip()[:100]
    if new_title:
        conversation.title = new_title
        conversation.save(update_fields=['title', 'updated_at'])

    conversations = Conversation.objects.filter(patient=patient, is_archived=False).order_by('-updated_at')
    return render(request, 'chat/partials/conversation_list.html', {
        'conversations': conversations,
    })


@require_GET
def conversation_list(request):
    if not request.user.is_authenticated:
        return HttpResponse('')

    try:
        patient = request.user.patient_profile
    except PatientProfile.DoesNotExist:
        return HttpResponse('')

    conversations = Conversation.objects.filter(patient=patient, is_archived=False)
    return render(request, 'chat/partials/conversation_list.html', {
        'conversations': conversations,
    })


@require_GET
def chat_sse(request):
    return _handle_chat_sse_request(request, SSEResponseStrategy())


def _handle_chat_sse_request(request, strategy: ChatResponseStrategy):
    message = request.GET.get('message', '').strip()
    conversation_id = request.GET.get('conversation_id')
    selected_dept_ids = request.GET.getlist('departments')

    if not message:
        return _sse_error_response(strategy, '参数错误', '请输入您的问题')

    if not selected_dept_ids:
        return _sse_error_response(strategy, '参数错误', '请选择咨询科室')

    if not request.user.is_authenticated:
        is_limited, limits = _is_anonymous_chat_rate_limited(request)
        if is_limited:
            return _anonymous_rate_limited_sse_response(strategy, limits)
        if not request.session.session_key:
            try:
                request.session.create()
            except Exception:
                logger.warning("Failed to create session for anonymous SSE user")

    service = container.chat_send_service
    result, lock_key = service.send_message(
        message=message,
        user=request.user,
        conversation_id=conversation_id,
        selected_dept_ids=selected_dept_ids,
        session_key=request.session.session_key,
    )

    if not result:
        return _sse_error_response(strategy, ChatErrorType.SYSTEM_UNAVAILABLE, '服务暂时不可用，请稍后再试', '请刷新页面重试')

    orchestrator = service.orchestrator

    if result.conversation and result.conversation.departments.exists():
        locked_depts = orchestrator.validate_department_lock(
            result.conversation, [int(d) for d in selected_dept_ids if d.isdigit()]
        )
        result.selected_departments = locked_depts

    def stream_response():
        try:
            yield strategy.generate_user_message(result.user_message_html)
            yield strategy.generate_status(result.system_hint)

            input_result = orchestrator.classify_input_safety(message)

            if input_result.is_blocked:
                if input_result.level == 'CRISIS':
                    crisis_response = input_result.crisis_response
                    html_content = markdown.markdown(crisis_response, extensions=['fenced_code', 'tables'])
                    safety_reason = ','.join(input_result.matched_keywords) if input_result.matched_keywords else '危机干预'
                    safety_msg = orchestrator.save_safety_message(result.conversation, crisis_response, 'CRISIS', safety_reason)
                    _record_crisis_event(result.conversation, safety_msg, 'CRISIS', message, input_result.matched_keywords, result.patient)
                    yield strategy.generate_sensitive_response(html_content)
                    yield strategy.generate_complete()
                    return
                elif input_result.level in ('INJECTION', 'BLOCKED'):
                    block_response = input_result.block_reason or '请求无法处理'
                    html_content = markdown.markdown(block_response, extensions=['fenced_code', 'tables'])
                    orchestrator.save_safety_message(
                        result.conversation, block_response, input_result.level,
                        input_result.block_reason or input_result.level,
                    )
                    yield strategy.generate_sensitive_response(html_content)
                    yield strategy.generate_complete()
                    return

            needs_emergency_reminder = input_result.needs_emergency_reminder

            yield strategy.generate_status('正在分析您的问题...')

            try:
                token_stream, relevant_chunks, should_warn, estimated_tokens, error_type = service.get_ai_response_stream(
                    message=message,
                    patient=result.patient,
                    selected_departments=result.selected_departments,
                    conversation_id=str(result.conversation.id) if result.conversation else None,
                )
            except Exception as e:
                logger.error("AI_RESPONSE_STREAM_FAILED | error=%s", str(e), exc_info=True)
                yield strategy.generate_error(
                    ChatErrorType.SYSTEM_UNAVAILABLE,
                    "AI 健康助手服务暂时不可用，请稍后再试。",
                    "请稍后刷新页面重试"
                )
                yield strategy.generate_complete()
                return

            is_rejection = not relevant_chunks

            if error_type == ChatErrorType.SYSTEM_UNAVAILABLE:
                yield strategy.generate_error(
                    ChatErrorType.SYSTEM_UNAVAILABLE,
                    "AI 健康助手服务暂时不可用，请稍后再试。",
                    "请稍后刷新页面重试"
                )
                yield strategy.generate_complete()
                return

            yield strategy.generate_status('正在生成回答...')

            ai_message = None
            if not is_rejection:
                ai_message = service.save_ai_message_streaming(
                    result.conversation,
                    relevant_chunks,
                )

            answer_tokens = []
            token_count = 0

            for token in token_stream:
                answer_tokens.append(token)
                token_count += 1
                if token_count % 50 == 0 and ai_message:
                    orchestrator.update_message_content(ai_message, ''.join(answer_tokens))
                yield strategy.generate_token(token)

            full_answer = ''.join(answer_tokens)

            if is_rejection:
                from apps.chat.models import Message
                Message.objects.create(
                    conversation=result.conversation,
                    sender=Message.Sender.AI,
                    content=full_answer,
                    processing_result=Message.ProcessingResult.REJECTED,
                )
                html_content = markdown.markdown(full_answer, extensions=['fenced_code', 'tables'])
                yield strategy.generate_ai_message(
                    html_content,
                    None,
                    [],
                    0,
                )
                yield strategy.generate_error(
                    ChatErrorType.NO_KNOWLEDGE,
                    None,
                    "如需专业医疗建议，请咨询您的主治医生"
                )
                yield strategy.generate_complete()
                return

            output_result = orchestrator.validate_output_safety(full_answer)

            original_content = ''
            safety_flag_level = ''
            safety_flag_reason = ''

            if output_result.level == 'BLOCKED':
                original_content = full_answer
                from apps.chat.strategies import OutputSafetyValidator
                full_answer = OutputSafetyValidator.BLOCKED_REPLACEMENT
                safety_flag_level = 'BLOCKED'
                safety_flag_reason = output_result.matched_pattern or 'AI越权输出'
            elif output_result.level == 'WARNING':
                full_answer = full_answer + "\n\n" + output_result.disclaimer_text
                safety_flag_level = 'WARNING'
                safety_flag_reason = output_result.matched_pattern or 'AI提及用药'

            if needs_emergency_reminder:
                full_answer = full_answer + "\n\n⚠️ 您描述的症状可能需要紧急处理，如症状持续或加重，请立即就医或拨打120。"
                _record_crisis_event(result.conversation, None, 'EMERGENCY', message, [], result.patient)

            if not is_rejection:
                service.finalize_ai_message(ai_message, full_answer, relevant_chunks)

            if not is_rejection and safety_flag_level:
                orchestrator.mark_message_safety(
                    ai_message, safety_flag_level, safety_flag_reason, original_content,
                )

            if not safety_flag_level and relevant_chunks:
                from apps.chat.services.answer_cache_service import AnswerCacheService
                dept_ids = [d.id for d in result.selected_departments] if result.selected_departments else []
                knowledge_version = AnswerCacheService.get_knowledge_version()
                cache_key = AnswerCacheService.get_cache_key(
                    message, result.patient, dept_ids, knowledge_version
                )
                AnswerCacheService.cache_answer(cache_key, full_answer, relevant_chunks)

            html_content = markdown.markdown(full_answer, extensions=['fenced_code', 'tables'])
            yield strategy.generate_ai_message(
                html_content,
                ai_message.id if ai_message else None,
                relevant_chunks if relevant_chunks else [],
                ai_message.feedback if ai_message else 0,
            )

            if should_warn:
                yield strategy.generate_context_warning(estimated_tokens)

            yield strategy.generate_complete()
        finally:
            service.release_lock(lock_key)

    response = StreamingHttpResponse(stream_response(), content_type=strategy.get_content_type())
    response['Cache-Control'] = 'no-cache'
    response['X-Accel-Buffering'] = 'no'
    return response


def _get_anonymous_chat_strategy(limits: dict) -> FixedWindowRateLimitStrategy:
    return FixedWindowRateLimitStrategy(
        limit=limits['limit'],
        window_seconds=limits['window'],
    )


def _get_anonymous_chat_identifier(request) -> str:
    ip = request.META.get('REMOTE_ADDR', 'unknown')
    return f"chat:anon:{ip}:{date.today().isoformat()}"


def _is_anonymous_chat_rate_limited(request) -> tuple[bool, dict]:
    try:
        limits = RateLimitConfigService.get_anonymous_chat_limits()
        strategy = _get_anonymous_chat_strategy(limits)
        identifier = _get_anonymous_chat_identifier(request)
        allowed, remaining = strategy.is_allowed(identifier)
        return not allowed, limits
    except Exception:
        logger.exception("Anonymous chat rate limit check failed, allowing request")
        return False, {}


def _sse_error_response(strategy: ChatResponseStrategy, error_type, message, suggestion=None):
    def error_stream():
        yield strategy.generate_error(error_type, message, suggestion)
        yield strategy.generate_complete()

    response = StreamingHttpResponse(
        error_stream(),
        content_type=strategy.get_content_type()
    )
    response['Cache-Control'] = 'no-cache'
    response['X-Accel-Buffering'] = 'no'
    return response


def _anonymous_rate_limited_sse_response(strategy: ChatResponseStrategy, limits: dict):
    retry_after = limits.get('window', 86400)
    rate_limit_data = {
        "message": "今日免费咨询次数已达上限，请注册账号后继续使用",
        "retry_after": retry_after,
    }

    def rate_limit_stream():
        yield f'event: rate_limited\ndata: {json.dumps(rate_limit_data, ensure_ascii=False)}\n\n'
        yield f'event: complete\ndata: {{"status": "done"}}\n\n'

    response = StreamingHttpResponse(
        rate_limit_stream(),
        content_type=strategy.get_content_type()
    )
    response['Cache-Control'] = 'no-cache'
    response['X-Accel-Buffering'] = 'no'
    return response


def _record_crisis_event(conversation, message, level, trigger_text, matched_keywords, patient):
    from apps.chat.models import CrisisEvent
    from apps.base.services import NotificationService
    from apps.base.models import Notification as NotificationModel

    try:
        event = CrisisEvent.objects.create(
            conversation=conversation,
            message=message,
            level=level,
            trigger_text=trigger_text[:500] if trigger_text else '',
            matched_keywords=','.join(matched_keywords) if matched_keywords else '',
            patient=patient,
        )

        notif_svc = NotificationService()
        dept_ids = conversation.departments.values_list('id', flat=True) if conversation.departments.exists() else []
        if dept_ids:
            from apps.base.models import Department
            for dept in Department.objects.filter(id__in=dept_ids):
                notif_svc.notify_department_staff(
                    department=dept,
                    category=NotificationModel.Category.CRISIS if level == 'CRISIS' else NotificationModel.Category.CRISIS,
                    title=f"{'自伤危机' if level == 'CRISIS' else '紧急症状'}事件",
                    content=f"患者{patient.name if patient else '匿名用户'}触发{level}级别安全事件，关键词：{','.join(matched_keywords) if matched_keywords else '无'}",
                    source_id=str(event.id),
                )
        elif patient:
            patient_dept_ids = patient.departments.values_list('id', flat=True)
            if patient_dept_ids:
                from apps.base.models import Department
                for dept in Department.objects.filter(id__in=patient_dept_ids):
                    notif_svc.notify_department_staff(
                        department=dept,
                        category=NotificationModel.Category.CRISIS,
                        title=f"{'自伤危机' if level == 'CRISIS' else '紧急症状'}事件",
                        content=f"患者{patient.name}触发{level}级别安全事件",
                        source_id=str(event.id),
                    )

        logger.info("CRISIS_EVENT_RECORDED | event_id=%s | level=%s | patient=%s", event.id, level, patient.id if patient else None)
    except Exception as e:
        logger.error("CRISIS_EVENT_RECORD_FAILED | reason=%s", str(e), exc_info=True)
