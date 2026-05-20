from django.http import JsonResponse
from django.views.decorators.http import require_POST
from django.views.decorators.csrf import ensure_csrf_cookie
from django.contrib.auth.decorators import login_required
from django.shortcuts import render

from apps.service_container import container


@require_POST
def message_feedback(request, message_id):
    if not request.user.is_authenticated:
        return JsonResponse({'error': '请先登录'}, status=401)

    try:
        feedback_value = int(request.POST.get('feedback', 0))
    except ValueError:
        return JsonResponse({'error': '无效的反馈值'}, status=400)

    reason = request.POST.get('reason', '')

    result = container.feedback_service.submit_feedback(
        message_id=message_id,
        user=request.user,
        feedback_value=feedback_value,
        reason=reason
    )

    if not result.success:
        status_code = 404 if result.error == "消息不存在" else 403
        return JsonResponse({'error': result.error}, status=status_code)

    return JsonResponse({
        'status': 'success',
        'feedback': result.message.feedback,
        'message_id': message_id,
    })


@login_required
def disliked_messages(request):
    """Disliked messages list view for staff review"""
    department_id = request.user.department_id if hasattr(request.user, 'department_id') else None
    messages = container.feedback_service.get_disliked_messages_with_details(department_id=department_id)

    return render(request, 'chat/disliked.html', {
        'disliked_messages': messages,
    })
