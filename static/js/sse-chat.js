(function() {
    'use strict';

    function SSEChatHandler(options) {
        this.messagesContainer = document.getElementById(options.messagesContainerId || 'chat-messages');
        this.inputElement = document.getElementById(options.inputId || 'message-input');
        this.sendButton = document.getElementById(options.sendButtonId || 'send-btn');
        this.conversationIdInput = document.querySelector('[name=conversation_id]');
        this.streamUrl = options.streamUrl || '/chat/stream/';
        this.currentEventSource = null;
        this.isStreaming = false;
        this.onStateChange = options.onStateChange || null;
    }

    SSEChatHandler.prototype.sendMessage = function(message, isInitial, departments) {
        if (this.isStreaming) return;

        var messageText = isInitial ? message : (this.inputElement ? this.inputElement.value.trim() : (message || ''));
        if (!messageText) return;

        var conversationId = this.conversationIdInput ? this.conversationIdInput.value : '';

        var selectedDepts = Array.isArray(departments) ? departments : [];
        if (selectedDepts.length === 0) {
            selectedDepts = Array.prototype.slice.call(document.querySelectorAll('input[name="departments"]:checked'))
                .map(function(input) {
                    return input.value;
                });
        }

        var url = this.streamUrl + '?message=' + encodeURIComponent(messageText) +
                  '&conversation_id=' + encodeURIComponent(conversationId);

        selectedDepts.forEach(function(deptId) {
            url += '&departments=' + encodeURIComponent(deptId);
        });

        // 立即清空输入框，不等待 AI 回复完成
        if (!isInitial && this.inputElement) {
            this.inputElement.value = '';
        }

        this.currentEventSource = new EventSource(url);
        this.isStreaming = true;
        this._updateUIState(true);

        var self = this;
        var eventSource = this.currentEventSource;

        eventSource.addEventListener('rate_limited', function(e) {
            self._rateLimitedHandled = true;
            var data = JSON.parse(e.data);
            self._showRateLimitMessage(data);
            self.isStreaming = false;
            self._updateUIState(false);
            if (self.onStateChange) {
                self.onStateChange({ isStreaming: false, rateLimited: true });
            }
        });

        eventSource.onmessage = function(e) {
            var data = JSON.parse(e.data);
            self._handleEvent(data, eventSource, isInitial);
        };

        eventSource.onerror = function(err) {
            eventSource.close();
            self.currentEventSource = null;
            self.isStreaming = false;
            self._updateUIState(false);

            if (self._rateLimitedHandled) {
                self._rateLimitedHandled = false;
                return;
            }

            if (self.messagesContainer) {
                var lastMsg = self.messagesContainer.lastElementChild;
                var hasExistingError = lastMsg && (
                    lastMsg.querySelector('.error-message') ||
                    lastMsg.textContent.indexOf('连接断开') !== -1
                );
                if (!hasExistingError) {
                    var errorDiv = document.createElement('div');
                    errorDiv.className = 'flex justify-center animate-fade-in';
                    errorDiv.innerHTML = '<div class="px-4 py-3 rounded-xl bg-red-50 border border-red-100 max-w-xs text-center">' +
                        '<p class="text-sm text-red-700 font-medium error-message">连接断开</p>' +
                        '<p class="text-xs text-red-500 mt-1">请刷新页面或重新发送消息</p>' +
                        '</div>';
                    self.messagesContainer.appendChild(errorDiv);
                    self._scrollToBottom();
                }
            }

            if (self.onStateChange) {
                self.onStateChange({ isStreaming: false, error: true });
            }
        };

        if (this.onStateChange) {
            this.onStateChange({ isStreaming: true });
        }
    };

    SSEChatHandler.prototype._showRateLimitMessage = function(data) {
        var limitMsg = document.createElement('div');
        limitMsg.className = 'flex justify-center my-4';
        var msg = (data && data.message) || '今日免费咨询次数已达上限';
        var loginUrl = (typeof window !== 'undefined' && window.LOGIN_URL) ? window.LOGIN_URL : '/accounts/login/';
        limitMsg.innerHTML = '<div class="text-sm text-center">' +
            '<div class="inline-flex items-center gap-2 px-4 py-2.5 rounded-full bg-amber-50 border border-amber-200 text-amber-700">' +
            '<svg class="w-4 h-4 flex-shrink-0" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 5a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 5zm0 9a1 1 0 100-2 1 1 0 000 2z" clip-rule="evenodd"/></svg>' +
            '<span>' + msg + '</span></div>' +
            '<div class="mt-2"><a href="' + loginUrl + '" class="text-sm text-sky-600 hover:text-sky-700 font-medium">立即注册/登录 &rarr;</a></div>' +
            '</div>';
        this.messagesContainer.appendChild(limitMsg);
        this._scrollToBottom();
    };

    SSEChatHandler.prototype._handleEvent = function(data, eventSource, isInitial) {
        if (!data || !data.type) return;

        switch (data.type) {
            case 'user_message':
                this._appendHTML(data.html);
                break;
            case 'status':
                this._updateStatus(data.text);
                if (data.departments && data.departments.length > 0) {
                    this._updateDepartments(data.departments);
                }
                break;
            case 'ai_token':
                this._appendAIToken(data.token);
                break;
            case 'ai_message':
                this._finalizeAIMessage(data.html);
                break;
            case 'context_warning':
                this._showContextWarning(data.estimated_tokens);
                break;
            case 'error':
                this._handleError(data);
                break;
            case 'complete':
                eventSource.close();
                this.currentEventSource = null;
                this.isStreaming = false;
                this._updateUIState(false);
                if (!isInitial && this.inputElement) {
                    this.inputElement.focus();
                }
                if (this.onStateChange) {
                    this.onStateChange({ isStreaming: false });
                }
                break;
            default:
                break;
        }
    };

    SSEChatHandler.prototype._appendHTML = function(html) {
        if (!html || !this.messagesContainer) return;
        var wrapper = document.createElement('div');
        wrapper.innerHTML = html;
        if (wrapper.firstElementChild) {
            this.messagesContainer.appendChild(wrapper.firstElementChild);
            this._scrollToBottom();
        }
    };

    SSEChatHandler.prototype._updateUIState = function(streaming) {
        if (this.inputElement) {
            this.inputElement.disabled = streaming;
            this.inputElement.placeholder = streaming ? 'AI 正在回复...' : '请输入您的问题...';
        }
        if (this.sendButton) {
            this.sendButton.disabled = streaming;
            var sendText = this.sendButton.querySelector('.send-text');
            var loading = this.sendButton.querySelector('.loading');
            if (sendText && loading) {
                if (streaming) {
                    sendText.classList.add('hidden');
                    loading.classList.remove('hidden');
                } else {
                    sendText.classList.remove('hidden');
                    loading.classList.add('hidden');
                }
            }
        }
    };

    SSEChatHandler.prototype._updateStatus = function(text) {
        var existingStatus = document.getElementById('streaming-status');
        if (existingStatus) {
            existingStatus.querySelector('.status-text').textContent = text;
        } else {
            var statusDiv = document.createElement('div');
            statusDiv.id = 'streaming-status';
            statusDiv.className = 'flex justify-center my-4';
            statusDiv.innerHTML = '<span class="text-xs text-slate-400 bg-slate-50 px-3 py-1.5 rounded-full status-text">' + text + '</span>';
            this.messagesContainer.appendChild(statusDiv);
            this._scrollToBottom();
        }
    };

    SSEChatHandler.prototype._updateDepartments = function(departmentNames) {
        var existingDept = document.getElementById('streaming-departments');
        if (existingDept) {
            existingDept.querySelector('.dept-list').innerHTML = departmentNames.map(function(name) {
                return '<span class="inline-flex items-center text-xs px-3 py-1.5 rounded-full bg-primary-light text-primary">' + name + '</span>';
            }).join('');
        } else {
            var deptDiv = document.createElement('div');
            deptDiv.id = 'streaming-departments';
            deptDiv.className = 'flex justify-center my-3';
            deptDiv.innerHTML = '<div class="text-xs text-slate-500 bg-slate-50 px-4 py-2 rounded-full">' +
                '<div class="flex items-center gap-2 mb-1"><span class="text-primary font-medium">正在检索以下科室知识库：</span></div>' +
                '<div class="dept-list flex flex-wrap gap-1.5">' + departmentNames.map(function(name) {
                    return '<span class="inline-flex items-center text-xs px-3 py-1.5 rounded-full bg-primary-light text-primary">' + name + '</span>';
                }).join('') + '</div></div>';
            this.messagesContainer.appendChild(deptDiv);
            this._scrollToBottom();
        }
    };

    SSEChatHandler.prototype._appendAIToken = function(token) {
        var aiBubble = document.getElementById('streaming-ai-bubble');
        if (!aiBubble) {
            var bubbleWrapper = document.createElement('div');
            bubbleWrapper.className = 'flex gap-3';
            bubbleWrapper.innerHTML = this._getAIBubbleHTML();
            this.messagesContainer.appendChild(bubbleWrapper);
        }
        aiBubble = document.getElementById('streaming-ai-bubble');
        if (aiBubble) {
            var contentDiv = aiBubble.querySelector('.ai-content');
            if (contentDiv) {
                contentDiv.textContent += token;
            }
        }
        this._scrollToBottom();
    };

    SSEChatHandler.prototype._getAIBubbleHTML = function() {
        return '<div id="streaming-ai-wrapper" class="flex gap-3">' +
               '<div class="w-10 h-10 rounded-full ai-avatar flex items-center justify-center flex-shrink-0">' +
               '<img src="/static/images/icons/sparkles.svg" class="w-5 h-5" alt="AI">' +
               '</div>' +
               '<div class="flex-1">' +
               '<div id="streaming-ai-bubble" class="bg-white rounded-2xl rounded-tl-sm p-4 shadow-sm border border-slate-100 max-w-[85%]">' +
               '<p class="text-sm font-medium mb-2 text-primary">智能健康助手</p>' +
               '<div class="ai-content text-slate-700 leading-relaxed"></div>' +
               '</div>' +
               '</div>' +
               '</div>';
    };

    SSEChatHandler.prototype._finalizeAIMessage = function(html) {
        var existingBubble = document.getElementById('streaming-ai-bubble');
        if (existingBubble) {
            var wrapper = document.getElementById('streaming-ai-wrapper');
            if (wrapper) {
                wrapper.outerHTML = html;
            } else {
                existingBubble.outerHTML = html;
            }
        }
        var statusEl = document.getElementById('streaming-status');
        if (statusEl) statusEl.remove();
        var deptEl = document.getElementById('streaming-departments');
        if (deptEl) deptEl.remove();
        this._scrollToBottom();

        // 通知 HTMX 处理动态插入的元素
        if (window.htmx && this.messagesContainer) {
            htmx.process(this.messagesContainer);
        }
    };

    SSEChatHandler.prototype._scrollToBottom = function() {
        if (this.messagesContainer) {
            this.messagesContainer.scrollTop = this.messagesContainer.scrollHeight;
        }
    };

    SSEChatHandler.prototype._showContextWarning = function(estimatedTokens) {
        if (!this.messagesContainer) return;
        var existing = document.getElementById('context-warning');
        if (existing) return;
        var warningDiv = document.createElement('div');
        warningDiv.id = 'context-warning';
        warningDiv.className = 'flex justify-center my-4';
        warningDiv.innerHTML = '<div class="text-sm text-center">' +
            '<div class="inline-flex items-center gap-2 px-4 py-2.5 rounded-full bg-blue-50 border border-blue-200 text-blue-700">' +
            '<svg class="w-4 h-4 flex-shrink-0" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z" clip-rule="evenodd"/></svg>' +
            '<span>对话内容较多，建议开启新会话以获得更好的体验</span></div>' +
            '</div>';
        this.messagesContainer.appendChild(warningDiv);
        this._scrollToBottom();
    };

    SSEChatHandler.prototype._handleError = function(data) {
        if (!this.messagesContainer) return;

        var errorType = data.error_type || 'unknown';
        var message = data.message || '';
        var suggestion = data.suggestion || '';

        if (errorType === 'no_knowledge' && !message && suggestion) {
            var tipContainer = document.createElement('div');
            tipContainer.className = 'flex justify-center my-2';
            tipContainer.innerHTML =
                '<div class="px-4 py-2 rounded-xl bg-amber-50 border border-amber-100 max-w-md text-center">' +
                '<div class="text-xs text-amber-600 opacity-75">' + suggestion + '</div>' +
                '</div>';
            this.messagesContainer.appendChild(tipContainer);
            this._scrollToBottom();
            if (this.onStateChange) {
                this.onStateChange({ isStreaming: false, error: true, errorType: errorType });
            }
            return;
        }

        if (!message) {
            message = '发生错误，请稍后再试';
        }
        if (!suggestion) {
            suggestion = '请刷新页面重试';
        }

        var errorContainer = document.createElement('div');
        errorContainer.className = 'flex justify-center my-4';

        var iconSvg = '';
        var bgClass = 'bg-red-50';
        var borderClass = 'border-red-100';
        var textClass = 'text-red-700';
        var iconClass = 'text-red-500';

        switch (errorType) {
            case 'system_unavailable':
                iconSvg = '<svg class="w-5 h-5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.28 7.22a.75.75 0 00-1.06 1.06L8.94 10l-1.72 1.72a.75.75 0 101.06 1.06L10 11.06l1.72 1.72a.75.75 0 101.06-1.06L11.06 10l1.72-1.72a.75.75 0 00-1.06-1.06L10 8.94 8.28 7.22z" clip-rule="evenodd"/></svg>';
                break;
            case 'no_knowledge':
                iconSvg = '<svg class="w-5 h-5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z" clip-rule="evenodd"/></svg>';
                bgClass = 'bg-amber-50';
                borderClass = 'border-amber-100';
                textClass = 'text-amber-700';
                iconClass = 'text-amber-500';
                break;
            case 'rate_limited':
                iconSvg = '<svg class="w-5 h-5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd"/></svg>';
                bgClass = 'bg-amber-50';
                borderClass = 'border-amber-100';
                textClass = 'text-amber-700';
                iconClass = 'text-amber-500';
                break;
            default:
                iconSvg = '<svg class="w-5 h-5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.28 7.22a.75.75 0 00-1.06 1.06L8.94 10l-1.72 1.72a.75.75 0 101.06 1.06L10 11.06l1.72 1.72a.75.75 0 101.06-1.06L11.06 10l1.72-1.72a.75.75 0 00-1.06-1.06L10 8.94 8.28 7.22z" clip-rule="evenodd"/></svg>';
        }

        errorContainer.innerHTML =
            '<div class="px-4 py-3 rounded-xl ' + bgClass + ' border ' + borderClass + ' max-w-md text-center">' +
            '<div class="flex items-center justify-center gap-2 ' + textClass + ' mb-2">' +
            '<span class="' + iconClass + '">' + iconSvg + '</span>' +
            '<span class="text-sm font-medium">' + message + '</span></div>';

        if (suggestion) {
            errorContainer.innerHTML +=
                '<div class="text-xs ' + textClass + ' opacity-75">' + suggestion + '</div>';
        }

        errorContainer.innerHTML += '</div>';

        this.messagesContainer.appendChild(errorContainer);
        this._scrollToBottom();

        if (this.onStateChange) {
            this.onStateChange({ isStreaming: false, error: true, errorType: errorType });
        }
    };

    SSEChatHandler.prototype.close = function() {
        if (this.currentEventSource) {
            this.currentEventSource.close();
            this.currentEventSource = null;
        }
        this.isStreaming = false;
        this._updateUIState(false);
    };

    window.SSEChatHandler = SSEChatHandler;

    // ---------- 事件委托：反馈 + 复制 ----------

    var UP_ACTIVE = 'feedback-btn feedback-up inline-flex items-center gap-0.5 text-[11px] px-2 py-1 rounded-full border-0 cursor-pointer text-sky-500 hover:text-sky-600 hover:bg-sky-50';
    var UP_INACTIVE = 'feedback-btn feedback-up inline-flex items-center gap-0.5 text-[11px] px-2 py-1 rounded-full border-0 cursor-pointer text-slate-400 hover:text-sky-600 hover:bg-sky-50 transition-colors';
    var DOWN_ACTIVE = 'feedback-btn feedback-down inline-flex items-center gap-0.5 text-[11px] px-2 py-1 rounded-full border-0 cursor-pointer text-red-500 hover:text-red-600 hover:bg-red-50';
    var DOWN_INACTIVE = 'feedback-btn feedback-down inline-flex items-center gap-0.5 text-[11px] px-2 py-1 rounded-full border-0 cursor-pointer text-slate-400 hover:text-red-600 hover:bg-red-50 transition-colors';

    document.addEventListener('click', function(e) {
        var feedbackBtn = e.target.closest('.feedback-btn');
        if (feedbackBtn) {
            handleFeedback(feedbackBtn);
            return;
        }
        var reasonBtn = e.target.closest('.feedback-reason-btn');
        if (reasonBtn) {
            handleReasonSelect(reasonBtn);
            return;
        }
    });

    function handleFeedback(btn) {
        var messageId = btn.getAttribute('data-message-id');
        var feedbackValue = parseInt(btn.getAttribute('data-feedback-value'), 10);
        if (!messageId || isNaN(feedbackValue)) return;

        if (feedbackValue === -1) {
            var panel = document.getElementById('feedback-reason-panel-' + messageId);
            if (panel) {
                document.querySelectorAll('[id^="feedback-reason-panel-"]').forEach(function(p) {
                    if (p !== panel) p.classList.add('hidden');
                });
                panel.classList.toggle('hidden');
            }
            return;
        }

        document.querySelectorAll('[id^="feedback-reason-panel-"]').forEach(function(p) {
            p.classList.add('hidden');
        });

        submitFeedback(messageId, feedbackValue);
    }

    function handleReasonSelect(btn) {
        var reason = btn.getAttribute('data-reason');
        var panel = btn.closest('[id^="feedback-reason-panel-"]');
        if (!panel) return;
        var messageId = panel.id.replace('feedback-reason-panel-', '');
        panel.classList.add('hidden');
        submitFeedback(messageId, -1, reason);
    }

    function submitFeedback(messageId, feedbackValue, reason) {
        var upBtn = document.getElementById('feedback-up-' + messageId);
        var downBtn = document.getElementById('feedback-down-' + messageId);
        if (!upBtn || !downBtn) return;

        var csrfInput = document.querySelector('[name=csrfmiddlewaretoken]');
        var csrfToken = csrfInput ? csrfInput.value : '';

        var formData = new FormData();
        formData.append('feedback', feedbackValue);
        if (reason) {
            formData.append('reason', reason);
        }

        fetch('/chat/feedback/' + messageId + '/', {
            method: 'POST',
            body: formData,
            headers: {'X-CSRFToken': csrfToken, 'X-Requested-With': 'XMLHttpRequest'},
            credentials: 'same-origin'
        })
        .then(function(r) {
            if (!r.ok) return r.json().catch(function() { return {error: '请求失败 (' + r.status + ')'}; });
            return r.json();
        })
        .then(function(data) {
            if (data.error) {
                var toast = document.createElement('div');
                toast.className = 'fixed bottom-4 right-4 bg-red-50 text-red-700 px-4 py-2 rounded-lg shadow-lg text-sm z-50';
                toast.textContent = data.error;
                document.body.appendChild(toast);
                setTimeout(function() { toast.remove(); }, 3000);
                return;
            }
            var f = data.feedback;
            if (f === 1) {
                upBtn.className = UP_ACTIVE;
                downBtn.className = DOWN_INACTIVE;
            } else if (f === -1) {
                upBtn.className = UP_INACTIVE;
                downBtn.className = DOWN_ACTIVE;
            } else {
                upBtn.className = UP_INACTIVE;
                downBtn.className = DOWN_INACTIVE;
            }
        })
        .catch(function(err) {
            console.error('Feedback request failed:', err);
            var toast = document.createElement('div');
            toast.className = 'fixed bottom-4 right-4 bg-red-50 text-red-700 px-4 py-2 rounded-lg shadow-lg text-sm z-50';
            toast.textContent = '反馈提交失败，请稍后重试';
            document.body.appendChild(toast);
            setTimeout(function() { toast.remove(); }, 3000);
        });
    }
})();