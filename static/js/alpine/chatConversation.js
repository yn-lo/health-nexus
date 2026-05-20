function chatConversation() {
    return {
        showConversationList: false,
        isSending: false,
        chatHandler: null,
        inputValue: '',
        selectedDepts: [],

        init() {
            var self = this;

            var sseUrlEl = this.$el;
            var streamUrl = (sseUrlEl && sseUrlEl.dataset.chatSseUrl) ? sseUrlEl.dataset.chatSseUrl : '/chat/stream/';

            var deptEl = document.getElementById('conversation-dept-ids-data');
            if (deptEl) {
                try { self.selectedDepts = JSON.parse(deptEl.textContent) || []; } catch (e) {}
            }

            self.chatHandler = new SSEChatHandler({
                streamUrl: streamUrl,
                onStateChange: function(state) {
                    self.isSending = state.isStreaming;
                }
            });

            var initialMessageInput = document.getElementById('initial-message');
            if (initialMessageInput && initialMessageInput.value) {
                var message = initialMessageInput.value;
                initialMessageInput.remove();
                var url = new URL(window.location.href);
                url.searchParams.delete('initial_message');
                window.history.replaceState({}, '', url.toString());
                self.chatHandler.sendMessage(message, true, self.selectedDepts);
            }
        },
        handleFormSubmit() {
            if (this.chatHandler) {
                this.chatHandler.sendMessage(null, false, this.selectedDepts);
            }
        }
    };
}
