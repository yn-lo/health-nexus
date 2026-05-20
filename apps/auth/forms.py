import re
from django import forms
from django.contrib.auth import get_user_model
from django.contrib.auth.forms import AuthenticationForm, PasswordChangeForm
from django.utils.translation import gettext_lazy as _

User = get_user_model()

INPUT_CSS_CLASS = 'appearance-none block w-full pl-11 pr-4 py-4 bg-slate-50 border border-slate-200 placeholder:text-slate-400 text-slate-900 rounded-xl focus:outline-none focus:ring-2 focus:ring-sky-500/20 focus:border-sky-500 text-lg transition-all'
PASSWORD_CSS_CLASS = 'appearance-none block w-full px-4 py-4 border border-gray-300 placeholder-gray-400 text-gray-900 rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500 focus:border-emerald-500 text-lg'


class H5LoginForm(AuthenticationForm):
    username = forms.CharField(
        label=_("用户名 / 手机号"),
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入用户名或手机号',
            'autocomplete': 'healthnexus-username',
        })
    )
    password = forms.CharField(
        label=_("密码"),
        widget=forms.PasswordInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入密码',
            'autocomplete': 'healthnexus-current-password',
        })
    )

    def clean(self):
        cleaned_data = super().clean()
        username = cleaned_data.get('username')
        if username:
            try:
                user = User.objects.get(username=username)
                if self._is_account_locked(user):
                    from django.core.cache import cache
                    lock_key = f"account_lock:{user.id}"
                    locked_until = cache.get(lock_key)
                    if locked_until:
                        from django.utils import timezone
                        remaining = int((locked_until - timezone.now().timestamp()) / 60) + 1
                        raise forms.ValidationError(
                            f"账户已锁定，请 {remaining} 分钟后再试"
                        )
            except User.DoesNotExist:
                pass
        return cleaned_data

    def _is_account_locked(self, user) -> bool:
        from django.core.cache import cache
        from django.utils import timezone
        lock_key = f"account_lock:{user.id}"
        locked_until = cache.get(lock_key)
        if locked_until and locked_until > timezone.now().timestamp():
            return True
        return False


class PhoneLoginForm(forms.Form):
    phone = forms.CharField(
        label=_("手机号"),
        max_length=20,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入手机号',
            'autocomplete': 'healthnexus-tel',
        })
    )
    verification_code = forms.CharField(
        label=_("验证码"),
        max_length=6,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入验证码',
            'autocomplete': 'healthnexus-one-time-code',
        })
    )

    def clean_phone(self):
        phone = self.cleaned_data.get('phone', '')
        digits = re.sub(r'\D', '', phone)
        if len(digits) < 11:
            raise forms.ValidationError(_("请输入有效的手机号"))
        return digits


class RegistrationForm(forms.ModelForm):
    phone = forms.CharField(
        label=_("手机号"),
        max_length=20,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入手机号',
            'autocomplete': 'healthnexus-tel',
        })
    )
    verification_code = forms.CharField(
        label=_("验证码"),
        max_length=6,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入验证码',
            'autocomplete': 'healthnexus-one-time-code',
        })
    )
    password1 = forms.CharField(
        label=_("密码"),
        widget=forms.PasswordInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请设置密码（8位以上，含字母和数字）',
            'autocomplete': 'healthnexus-new-password',
        })
    )
    password2 = forms.CharField(
        label=_("确认密码"),
        widget=forms.PasswordInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请再次输入密码',
            'autocomplete': 'healthnexus-new-password',
        })
    )
    agreed_terms = forms.BooleanField(
        label=_("我已阅读并同意《知情同意书与免责声明》"),
        required=True,
        error_messages={
            'required': _('您必须同意免责条款才能注册'),
        },
        widget=forms.CheckboxInput(attrs={
            'class': 'w-5 h-5 text-emerald-600 border-gray-300 rounded focus:ring-emerald-500',
        })
    )

    class Meta:
        model = User
        fields = ['username']
        widgets = {
            'username': forms.TextInput(attrs={
                'class': INPUT_CSS_CLASS,
                'placeholder': '请设置用户名',
                'autocomplete': 'healthnexus-username',
            }),
        }

    def clean_phone(self):
        phone = self.cleaned_data.get('phone', '')
        digits = re.sub(r'\D', '', phone)
        if len(digits) < 11:
            raise forms.ValidationError(_("请输入有效的手机号"))
        return digits

    def clean_password1(self):
        password1 = self.cleaned_data.get('password1')
        if password1:
            if len(password1) < 8:
                raise forms.ValidationError(_("密码长度至少8位"))
            if not re.search(r'[A-Za-z]', password1):
                raise forms.ValidationError(_("密码必须包含字母"))
            if not re.search(r'\d', password1):
                raise forms.ValidationError(_("密码必须包含数字"))
        return password1

    def clean_password2(self):
        password1 = self.cleaned_data.get('password1')
        password2 = self.cleaned_data.get('password2')
        if password1 and password2 and password1 != password2:
            raise forms.ValidationError(_("两次输入的密码不一致"))
        return password2

    def clean_username(self):
        username = self.cleaned_data.get('username')
        if User.objects.filter(username=username).exists():
            raise forms.ValidationError(_("该用户名已被注册"))
        return username

    def clean_verification_code(self):
        from .services.sms_service import SMSService
        sms_service = SMSService()
        phone = self.cleaned_data.get('phone')
        code = self.cleaned_data.get('verification_code')
        if phone and code:
            if not sms_service.check_code(phone, code):
                raise forms.ValidationError(_("验证码错误或已过期"))
        return code

    def save(self, commit=True):
        user = super().save(commit=False)
        user.set_password(self.cleaned_data['password1'])
        user.phone = self.cleaned_data.get('phone')
        user.role = User.Role.PATIENT
        user.is_staff = False
        user.is_superuser = False
        if self.cleaned_data.get('agreed_terms'):
            from django.utils import timezone
            user.agreed_terms = True
            user.agreed_terms_at = timezone.now()
        if commit:
            user.save()
            from .services.sms_service import SMSService
            SMSService().consume_code(self.cleaned_data.get('phone'))
        return user


class H5PasswordChangeForm(PasswordChangeForm):
    old_password = forms.CharField(
        label=_("当前密码"),
        widget=forms.PasswordInput(attrs={
            'class': PASSWORD_CSS_CLASS,
            'placeholder': '请输入当前密码',
            'autocomplete': 'current-password',
        })
    )
    new_password1 = forms.CharField(
        label=_("新密码"),
        widget=forms.PasswordInput(attrs={
            'class': PASSWORD_CSS_CLASS,
            'placeholder': '请输入新密码（8位以上，含字母和数字）',
            'autocomplete': 'new-password',
        })
    )
    new_password2 = forms.CharField(
        label=_("确认新密码"),
        widget=forms.PasswordInput(attrs={
            'class': PASSWORD_CSS_CLASS,
            'placeholder': '请再次输入新密码',
            'autocomplete': 'new-password',
        })
    )


class TermsAgreementForm(forms.Form):
    agreed_terms = forms.BooleanField(
        label=_("我已阅读并同意《知情同意书与免责声明》"),
        required=True,
        error_messages={
            'required': _('您必须同意免责条款才能继续使用'),
        },
        widget=forms.CheckboxInput(attrs={
            'class': 'w-5 h-5 text-emerald-600 border-gray-300 rounded focus:ring-emerald-500',
        })
    )


class PhoneBindingForm(forms.Form):
    phone = forms.CharField(
        label=_("手机号"),
        max_length=20,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入新手机号',
            'autocomplete': 'healthnexus-tel',
        })
    )
    verification_code = forms.CharField(
        label=_("验证码"),
        max_length=6,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入验证码',
            'autocomplete': 'healthnexus-one-time-code',
        })
    )

    def clean_phone(self):
        phone = self.cleaned_data.get('phone', '')
        digits = re.sub(r'\D', '', phone)
        if len(digits) != 11:
            raise forms.ValidationError(_("请输入有效的11位手机号"))
        return digits

    def clean_verification_code(self):
        code = self.cleaned_data.get('verification_code', '')
        if not code.isdigit() or len(code) != 6:
            raise forms.ValidationError(_("请输入6位数字验证码"))
        return code


class PhoneUnbindForm(forms.Form):
    verification_code = forms.CharField(
        label=_("验证码"),
        max_length=6,
        widget=forms.TextInput(attrs={
            'class': INPUT_CSS_CLASS,
            'placeholder': '请输入验证码',
            'autocomplete': 'healthnexus-one-time-code',
        })
    )

    def clean_verification_code(self):
        code = self.cleaned_data.get('verification_code', '')
        if not code.isdigit() or len(code) != 6:
            raise forms.ValidationError(_("请输入6位数字验证码"))
        return code


class SecuritySettingsForm(forms.Form):
    login_alert_enabled = forms.BooleanField(
        label=_("登录提醒开关"),
        required=False,
        widget=forms.CheckboxInput(attrs={
            'class': 'w-5 h-5 text-emerald-600 border-gray-300 rounded focus:ring-emerald-500',
        })
    )


class PrivacySettingsForm(forms.Form):
    health_data_share_enabled = forms.BooleanField(
        label=_("健康数据共享开关"),
        required=False,
        widget=forms.CheckboxInput(attrs={
            'class': 'w-5 h-5 text-emerald-600 border-gray-300 rounded focus:ring-emerald-500',
        })
    )


class AvatarUploadForm(forms.Form):
    avatar = forms.ImageField(
        label=_("头像"),
        required=True,
    )

    def clean_avatar(self):
        avatar = self.cleaned_data.get('avatar')
        if avatar:
            if avatar.size > 5 * 1024 * 1024:
                raise forms.ValidationError(_("头像大小不能超过 5MB"))
            allowed_extensions = ['jpg', 'jpeg', 'png', 'gif', 'webp']
            ext = avatar.name.split('.')[-1].lower() if '.' in avatar.name else ''
            if ext not in allowed_extensions:
                raise forms.ValidationError(_("仅支持 JPG/PNG/GIF/WebP 格式"))
        return avatar


class PreferencesForm(forms.Form):
    THEME_CHOICES = [
        ('light', '浅色'),
        ('dark', '深色'),
        ('system', '跟随系统'),
    ]
    LANGUAGE_CHOICES = [
        ('zh-CN', '简体中文'),
        ('en', 'English'),
    ]
    FONT_SIZE_CHOICES = [
        ('small', '小'),
        ('medium', '中'),
        ('large', '大'),
    ]

    theme = forms.ChoiceField(
        label=_("界面主题"),
        choices=THEME_CHOICES,
        required=False,
        initial='light',
    )
    language = forms.ChoiceField(
        label=_("界面语言"),
        choices=LANGUAGE_CHOICES,
        required=False,
        initial='zh-CN',
    )
    notification_enabled = forms.BooleanField(
        label=_("接收系统通知"),
        required=False,
        initial=True,
    )
    font_size = forms.ChoiceField(
        label=_("字体大小"),
        choices=FONT_SIZE_CHOICES,
        required=False,
        initial='medium',
    )
