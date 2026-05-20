from django.urls import path
from . import views

app_name = 'chat'

urlpatterns = [
    path('', views.chat_home, name='chat_home'),
    path('new/', views.chat_new, name='chat_new'),
    path('conversation/<uuid:conversation_id>/', views.chat_conversation, name='chat_conversation'),
    path('conversation/<uuid:conversation_id>/delete/', views.conversation_delete, name='conversation_delete'),
    path('conversation/<uuid:conversation_id>/rename/', views.conversation_rename, name='conversation_rename'),
    path('stream/', views.chat_sse, name='chat_sse'),
    path('feedback/<int:message_id>/', views.message_feedback, name='feedback'),
    path('disliked/', views.disliked_messages, name='disliked'),
    path('conversations/', views.conversation_list, name='conversation_list'),
]
