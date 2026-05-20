function confirmDialog() {
    return {
        open: false,
        title: '',
        message: '',
        danger: false,
        postUrl: '',
        target: '',
        redirectUrl: '',
        show(detail) {
            if (!detail) return;
            this.title = detail.title || '确认';
            this.message = detail.message || '';
            this.danger = detail.danger || false;
            this.postUrl = detail.postUrl || '';
            this.target = detail.target || 'body';
            this.redirectUrl = detail.redirectUrl || '';
            if (detail.action) {
                console.warn('[confirmDialog] action 属性已废弃，请使用 $dispatch 事件机制替代');
            }
            this.open = true;
        },
        close() {
            this.open = false;
        },
        closeIfOpen() {
            if (this.open) this.close();
        },
        confirm() {
            this.$dispatch('confirm-dialog-confirmed');
            this.close();
        },
        confirmWithPost() {
            var postUrl = this.postUrl;
            var target = this.target;
            var redirectUrl = this.redirectUrl;
            this.close();
            var self = this;
            this.$nextTick(function() {
                apiPostHtml(postUrl)
                    .then(function(result) {
                        if (!result.ok) {
                            alert('操作失败: ' + (result.error ? result.error.message : '未知错误'));
                            return;
                        }
                        if (redirectUrl) {
                            window.location.href = redirectUrl;
                            return;
                        }
                        if (result.html) {
                            var targetSelector = target.startsWith('#') ? target : '#' + target;
                            var targetEl = document.querySelector(targetSelector);
                            if (targetEl) {
                                targetEl.innerHTML = result.html;
                                if (window.htmx) {
                                    htmx.process(targetEl);
                                }
                            }
                        }
                    })
                    .catch(function(err) {
                        console.error('操作失败:', err);
                        alert('操作失败，请重试');
                    });
            });
        }
    };
}
