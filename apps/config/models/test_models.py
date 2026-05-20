from django.db import models
from apps.config.models import BaseConfig


class TestConfig(BaseConfig):
    """Concrete model for testing BaseConfig abstract class"""
    name = models.CharField(max_length=50)
    value = models.CharField(max_length=200, blank=True, default="")

    class Meta:
        app_label = 'config'

    def __str__(self):
        return self.name
