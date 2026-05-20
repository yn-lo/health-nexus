import os

from django.contrib.auth import get_user_model
from django.core.management.base import BaseCommand


User = get_user_model()


class Command(BaseCommand):
    help = "Create superuser if not exists (for Docker deployment)"

    def handle(self, *args, **options):
        username = os.environ.get("SUPERUSER_USERNAME", "admin")
        email = os.environ.get("SUPERUSER_EMAIL", "admin@example.com")
        password = os.environ.get("SUPERUSER_PASSWORD", "")

        if not password:
            self.stdout.write(self.style.WARNING("SUPERUSER_PASSWORD not set, skipping superuser creation"))
            return

        if User.objects.filter(username=username).exists():
            self.stdout.write(self.style.SUCCESS(f"Superuser '{username}' already exists, skipping"))
            return

        User.objects.create_superuser(username=username, email=email, password=password)
        self.stdout.write(self.style.SUCCESS(f"Superuser '{username}' created successfully"))
