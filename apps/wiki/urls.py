from django.urls import path
from . import views
from . import views_staff

app_name = 'wiki'

urlpatterns = [
    path('', views.blog_list, name='blog_list'),
    path('<int:pk>/', views.article_detail, name='article_detail'),

    path('staff/articles/', views_staff.article_management, name='article_management'),
    path('staff/articles/create/', views_staff.article_create, name='article_create'),
    path('staff/articles/<int:pk>/edit/', views_staff.article_edit, name='article_edit'),
    path('staff/articles/<int:pk>/submit/', views_staff.article_submit_review, name='article_submit_review'),
    path('staff/articles/<int:pk>/delete/', views_staff.article_delete_draft, name='article_delete_draft'),

    path('staff/review/', views_staff.article_review_list, name='review_list'),
    path('staff/review/approve/', views_staff.article_review_action, name='review_approve'),
    path('staff/review/reject/', views_staff.article_review_action, name='review_reject'),
    path('staff/re-review/', views_staff.re_review_list, name='re_review_list'),
    path('staff/re-review/approve/', views_staff.re_review_action, name='re_review_approve'),
    path('staff/re-review/reject/', views_staff.re_review_action, name='re_review_reject'),
]
