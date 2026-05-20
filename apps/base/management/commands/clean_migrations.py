"""Clean all migration files management command.

Deletes all auto-generated migration files across the project,
keeping only __init__.py files in migrations directories.
Only affects apps inside the project root, skipping third-party packages.

Usage:
    python manage.py clean_migrations
    python manage.py clean_migrations --dry-run
"""

import os
from pathlib import Path

from django.apps import apps
from django.conf import settings
from django.core.management.base import BaseCommand


class Command(BaseCommand):
    help = "Delete all migration files (keep __init__.py) across project apps"

    def add_arguments(self, parser):
        parser.add_argument(
            "--dry-run",
            action="store_true",
            help="Show what would be deleted without actually deleting",
        )

    def handle(self, *args, **options):
        dry_run = options["dry_run"]
        deleted_count = 0

        project_root = Path(settings.BASE_DIR).resolve()
        app_configs = apps.get_app_configs()

        for app_config in app_configs:
            app_path = Path(app_config.path).resolve()
            if not str(app_path).startswith(str(project_root)):
                continue

            migrations_dir = app_path / "migrations"
            if not migrations_dir.exists():
                continue

            for file_path in migrations_dir.iterdir():
                if not file_path.is_file():
                    continue
                if file_path.name == "__init__.py":
                    continue

                if dry_run:
                    self.stdout.write(
                        self.style.WARNING(f"[DRY-RUN] Would delete: {file_path}")
                    )
                else:
                    file_path.unlink()
                    self.stdout.write(
                        self.style.SUCCESS(f"Deleted: {file_path}")
                    )
                deleted_count += 1

            pycache_dir = migrations_dir / "__pycache__"
            if pycache_dir.exists():
                for file_path in pycache_dir.iterdir():
                    if file_path.is_file():
                        if dry_run:
                            self.stdout.write(
                                self.style.WARNING(
                                    f"[DRY-RUN] Would delete: {file_path}"
                                )
                            )
                        else:
                            file_path.unlink()
                        deleted_count += 1
                if not dry_run:
                    try:
                        pycache_dir.rmdir()
                        self.stdout.write(
                            f"Removed empty pycache: {pycache_dir}"
                        )
                    except OSError:
                        pass

        action = "Would delete" if dry_run else "Deleted"
        self.stdout.write(
            f"\n{action} {deleted_count} migration file(s)."
        )
