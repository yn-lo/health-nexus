/**
 * SMS verification code utility
 * Provides unified SMS code sending and countdown timer functionality.
 */

/**
 * Send SMS verification code and start countdown.
 * @param {HTMLElement} btnEl - The button element to disable and show countdown
 * @param {string} phoneInputId - ID of the phone input element
 * @param {string} [smsType] - SMS type (e.g., 'register', 'login', 'reset_password')
 */
function sendSmsCode(btnEl, phoneInputId, smsType) {
    var phoneInput = document.getElementById(phoneInputId);
    if (!phoneInput) {
        alert('页面错误：找不到手机号输入框');
        return;
    }

    var phone = phoneInput.value.trim();
    if (!phone || phone.length < 11) {
        alert('请先输入有效的手机号');
        return;
    }

    btnEl.disabled = true;
    btnEl.textContent = '发送中...';

    var body = { phone: phone };
    if (smsType) {
        body.type = smsType;
    }

    fetch('/accounts/send-sms/', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRFToken': getCSRFToken()
        },
        body: JSON.stringify(body)
    })
    .then(function(response) { return response.json(); })
    .then(function(data) {
        if (data.success) {
            var msg = '验证码已发送';
            if (data.debug_code) {
                msg += '\n【测试模式】验证码: ' + data.debug_code;
            }
            alert(msg);
            startSmsCountdown(btnEl);
        } else {
            alert(data.error || '发送失败');
            btnEl.disabled = false;
            btnEl.textContent = '获取验证码';
        }
    })
    .catch(function() {
        alert('网络错误，请重试');
        btnEl.disabled = false;
        btnEl.textContent = '获取验证码';
    });
}

/**
 * Start a 60-second countdown timer on the button.
 * @param {HTMLElement} btnEl - The button element
 */
function startSmsCountdown(btnEl) {
    var countdown = 60;
    var timer = setInterval(function() {
        btnEl.textContent = countdown + '秒后重试';
        countdown--;
        if (countdown < 0) {
            clearInterval(timer);
            btnEl.disabled = false;
            btnEl.textContent = '获取验证码';
        }
    }, 1000);
}
