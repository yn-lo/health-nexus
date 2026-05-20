import re
from django.shortcuts import render, redirect
from django.contrib.auth import update_session_auth_hash
from django.contrib import messages
from django import forms

from apps.auth.models import UserProfile
from apps.auth.decorators import staff_member_required


class StaffProfileForm(forms.ModelForm):
    class Meta:
        model = UserProfile
        fields = ['avatar', 'title', 'bio']
        widgets = {
            'title': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3',
            }),
            'bio': forms.Textarea(attrs={
                'rows': 4,
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3',
                'placeholder': '介绍一下您的专业背景...',
            }),
        }


@staff_member_required
def staff_profile_view(request):
    user = request.user
    departments = user.departments.all()
    return render(request, 'staff/profile.html', {
        'user': user,
        'user_role_display': dict(UserProfile.Role.choices).get(user.role, ''),
        'departments': departments,
    })


@staff_member_required
def staff_profile_edit_view(request):
    user = request.user
    if request.method == 'POST':
        title = request.POST.get('title', '')
        bio = request.POST.get('bio', '')
        user.title = title
        user.bio = bio
        user.save(update_fields=['title', 'bio'])
        messages.success(request, '个人信息已更新')
        return redirect('staff:profile')

    return render(request, 'staff/profile_edit.html', {
        'user': user,
        'title_choices': UserProfile.Title.choices if hasattr(UserProfile, 'Title') else [],
    })


@staff_member_required
def staff_profile_security_view(request):
    if request.method == 'POST':
        old_password = request.POST.get('old_password', '')
        new_password = request.POST.get('new_password', '')
        confirm_password = request.POST.get('confirm_password', '')

        if not request.user.check_password(old_password):
            messages.error(request, '原密码不正确')
        elif new_password != confirm_password:
            messages.error(request, '两次输入的新密码不一致')
        elif len(new_password) < 8:
            messages.error(request, '密码长度不能少于8位')
        elif not re.search(r'[A-Za-z]', new_password):
            messages.error(request, '密码必须包含字母')
        elif not re.search(r'\d', new_password):
            messages.error(request, '密码必须包含数字')
        else:
            request.user.set_password(new_password)
            request.user.save()
            update_session_auth_hash(request, request.user)
            messages.success(request, '密码修改成功')
            return redirect('staff:profile_security')

    return render(request, 'staff/profile_security.html', {'user': request.user})
