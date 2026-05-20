function renameDialog() {
    return {
        open: false,
        title: '',
        postUrl: '',
        inputTitle: '',
        target: '',
        show(detail) {
            if (!detail) return;
            this.title = detail.title || '重命名';
            this.postUrl = detail.postUrl || '';
            this.inputTitle = detail.currentTitle || '';
            this.target = detail.target || 'body';
            this.open = true;
            var self = this;
            this.$nextTick(function() {
                if (self.$refs.titleInput) {
                    self.$refs.titleInput.focus();
                    self.$refs.titleInput.select();
                }
            });
        },
        close() {
            this.open = false;
        },
        submit() {
            if (!this.inputTitle.trim()) return;
            var postUrl = this.postUrl;
            var target = this.target;
            var newTitle = this.inputTitle.trim();
            this.close();
            this.$nextTick(function() {
                var formData = new FormData();
                formData.append('title', newTitle);
                apiPostHtml(postUrl, formData)
                    .then(function(result) {
                        if (!result.ok) {
                            alert('操作失败: ' + (result.error ? result.error.message : '未知错误'));
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
