function chatHome() {
    var defaultQuestions = [
        '什么是高血压？有哪些常见症状？',
        '如何预防糖尿病？日常饮食要注意什么？',
        '感冒和流感有什么区别？'
    ];

    var deptEl = document.getElementById('patient-dept-ids-data');
    var publicEl = document.getElementById('public-dept-ids-data');
    var initialDepts = [];
    var publicDepts = [];
    if (deptEl) {
        try { initialDepts = JSON.parse(deptEl.textContent) || []; } catch (e) {}
    }
    if (publicEl) {
        try { publicDepts = JSON.parse(publicEl.textContent) || []; } catch (e) {}
    }

    return {
        searchQuery: '',
        showDepartmentFilters: false,
        selectedDepartment: '',
        showHistory: false,
        showDeptSelector: false,
        inputValue: '',
        isSubmitting: false,
        suggestedQuestions: window.__HOT_QUESTIONS__ || defaultQuestions,
        currentDeptIds: initialDepts.concat(publicDepts),
        deptValidationError: '',

        toggleDepartment(id) {
            if (this.selectedDepartment === id) {
                this.selectedDepartment = '';
            } else {
                this.selectedDepartment = id;
            }
        },

        deleteConversation(event) {
            var id = event.currentTarget.dataset.deleteId;
            if (!id) return;
            this.$dispatch('show-confirm-dialog', {
                title: '删除对话',
                message: '确定删除此对话吗？此操作不可撤销。',
                danger: true,
                postUrl: '/chat/conversations/' + id + '/delete/',
                redirectUrl: '/chat/'
            });
        },

        askQuestion(question) {
            this.inputValue = question;
            this.handleSubmit();
        },

        handleSubmit() {
            var self = this;
            if (!self.inputValue.trim() || self.isSubmitting) return;

            self.deptValidationError = '';

            if (self.currentDeptIds.length === 0) {
                self.deptValidationError = '请至少选择一个知识库科室';
                return;
            }

            self.isSubmitting = true;

            var url = '/chat/new/?message=' + encodeURIComponent(self.inputValue);
            self.currentDeptIds.forEach(function(deptId) {
                url += '&departments=' + encodeURIComponent(deptId);
            });

            window.location.href = url;
        },

        init() {
            var self = this;
            window.addEventListener('dept-selector:close', function() {
                self.showDeptSelector = false;
            });
            window.addEventListener('dept-selector:saved', function(e) {
                self.currentDeptIds = e.detail.departments || [];
            });
        }
    };
}
