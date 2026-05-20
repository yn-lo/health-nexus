"""Deployment readiness check management command.

Verifies production configuration against the deployment requirements
defined in docs/requirements/08-deployment.md.

Usage:
    python manage.py check_deployment
    python manage.py check_deployment --verbose
"""

import re

from django.conf import settings
from django.core.management.base import BaseCommand
from django.db import connection


class Command(BaseCommand):
    help = "Check deployment readiness against production requirements"

    def add_arguments(self, parser):
        parser.add_argument(
            "--verbose",
            action="store_true",
            help="Show detailed check results including passed checks",
        )

    def handle(self, *args, **options):
        self.verbose = options["verbose"]
        self.results = {"P0": [], "P1": [], "P2": []}

        self._check_secrets()
        self._check_debug()
        self._check_allowed_hosts()
        self._check_csrf_trusted_origins()
        self._check_cookie_security()
        self._check_ssl_redirect()
        self._check_database()
        self._check_redis()
        self._check_dependency_versions()
        self._check_hsts()

        self._print_report()

    def _record(self, priority, check_id, description, passed, detail=""):
        self.results[priority].append({
            "id": check_id,
            "description": description,
            "passed": passed,
            "detail": detail,
        })

    def _check_secrets(self):
        secret_key = settings.SECRET_KEY
        is_insecure = any(
            kw in secret_key.lower()
            for kw in ["change-me", "insecure", "example", "django-insecure"]
        )
        self._record(
            "P0", "CHK-P0-01", "SECRET_KEY 已轮换",
            not is_insecure and len(secret_key) >= 50,
            f"长度={len(secret_key)}, 包含默认值={is_insecure}",
        )

        encryption_key = settings.FIELD_ENCRYPTION_KEY
        is_insecure_enc = any(
            kw in encryption_key.lower()
            for kw in ["change-me", "example", "your-"]
        )
        self._record(
            "P0", "CHK-P0-02", "FIELD_ENCRYPTION_KEY 已轮换",
            not is_insecure_enc,
            f"包含默认值={is_insecure_enc}",
        )

        db_password = os.environ.get("DB_PASSWORD", "postgres") if __import__("os") else "postgres"
        is_insecure_db = db_password in ("postgres", "CHANGE-ME", "change-me")
        self._record(
            "P0", "CHK-P0-03", "数据库密码已轮换",
            not is_insecure_db and len(db_password) >= 16,
            f"长度={len(db_password)}, 使用默认值={is_insecure_db}",
        )

        superuser_pw = os.environ.get("SUPERUSER_PASSWORD", "") if __import__("os") else ""
        is_insecure_su = superuser_pw in ("CHANGE-ME", "admin", "change-me", "")
        self._record(
            "P0", "CHK-P0-03b", "超级用户密码已轮换",
            not is_insecure_su and len(superuser_pw) >= 12,
            f"长度={len(superuser_pw)}, 使用默认值={is_insecure_su}",
        )

    def _check_debug(self):
        self._record(
            "P0", "CHK-P0-08", "DEBUG=False",
            not settings.DEBUG,
            f"DEBUG={settings.DEBUG}",
        )

    def _check_allowed_hosts(self):
        has_wildcard = "*" in settings.ALLOWED_HOSTS
        has_localhost = "localhost" in settings.ALLOWED_HOSTS or "127.0.0.1" in settings.ALLOWED_HOSTS
        self._record(
            "P0", "CHK-P0-08b", "ALLOWED_HOSTS 无通配符",
            not has_wildcard,
            f"ALLOWED_HOSTS={settings.ALLOWED_HOSTS}",
        )
        self._record(
            "P1", "CHK-P1-01", "ALLOWED_HOSTS 不含 localhost",
            not has_localhost,
            f"包含localhost/127.0.0.1={has_localhost}",
        )

    def _check_csrf_trusted_origins(self):
        origins = settings.CSRF_TRUSTED_ORIGINS
        has_localhost = any("localhost" in o or "127.0.0.1" in o for o in origins)
        has_https = any(o.startswith("https://") for o in origins)
        self._record(
            "P1", "CHK-P1-02", "CSRF_TRUSTED_ORIGINS 不含 localhost",
            not has_localhost,
            f"CSRF_TRUSTED_ORIGINS={origins}",
        )
        self._record(
            "P1", "CHK-P1-03", "CSRF_TRUSTED_ORIGINS 包含 HTTPS 来源",
            has_https,
            f"包含https来源={has_https}",
        )

    def _check_cookie_security(self):
        self._record(
            "P0", "CHK-P0-08c", "SESSION_COOKIE_SECURE=True",
            settings.SESSION_COOKIE_SECURE,
            f"SESSION_COOKIE_SECURE={settings.SESSION_COOKIE_SECURE}",
        )
        self._record(
            "P0", "CHK-P0-08d", "CSRF_COOKIE_SECURE=True",
            settings.CSRF_COOKIE_SECURE,
            f"CSRF_COOKIE_SECURE={settings.CSRF_COOKIE_SECURE}",
        )
        self._record(
            "P1", "CHK-P1-04", "SESSION_COOKIE_HTTPONLY=True",
            settings.SESSION_COOKIE_HTTPONLY,
            f"SESSION_COOKIE_HTTPONLY={settings.SESSION_COOKIE_HTTPONLY}",
        )
        self._record(
            "P1", "CHK-P1-05", "CSRF_COOKIE_HTTPONLY=True",
            settings.CSRF_COOKIE_HTTPONLY,
            f"CSRF_COOKIE_HTTPONLY={settings.CSRF_COOKIE_HTTPONLY}",
        )

    def _check_ssl_redirect(self):
        ssl_redirect = getattr(settings, "SECURE_SSL_REDIRECT", False)
        self._record(
            "P1", "CHK-P1-06", "SECURE_SSL_REDIRECT=True",
            ssl_redirect,
            f"SECURE_SSL_REDIRECT={ssl_redirect}",
        )

    def _check_database(self):
        try:
            connection.ensure_connection()
            with connection.cursor() as cursor:
                cursor.execute("SELECT 1")
                cursor.fetchone()
            self._record("P0", "CHK-P0-10", "数据库连接正常", True)
        except Exception as e:
            self._record("P0", "CHK-P0-10", "数据库连接正常", False, str(e))

    def _check_redis(self):
        try:
            from django.core.cache import cache
            cache.set("_deploy_check", "ok", 5)
            value = cache.get("_deploy_check")
            self._record(
                "P0", "CHK-P0-04", "Redis 连接正常",
                value == "ok",
                f"读写测试={'通过' if value == 'ok' else '失败'}",
            )
        except Exception as e:
            self._record("P0", "CHK-P0-04", "Redis 连接正常", False, str(e))

    def _check_dependency_versions(self):
        try:
            import qrcode
            import openpyxl
            qrcode_version = getattr(qrcode, "__version__", "unknown")
            openpyxl_version = getattr(openpyxl, "__version__", "unknown")
            self._record(
                "P1", "CHK-P1-07", "qrcode 版本已锁定",
                qrcode_version != "unknown",
                f"qrcode=={qrcode_version}",
            )
            self._record(
                "P1", "CHK-P1-08", "openpyxl 版本已锁定",
                openpyxl_version != "unknown",
                f"openpyxl=={openpyxl_version}",
            )
        except ImportError:
            self._record("P1", "CHK-P1-07", "依赖版本检查", False, "无法导入依赖")

    def _check_hsts(self):
        hsts = getattr(settings, "SECURE_HSTS_SECONDS", 0)
        self._record(
            "P1", "CHK-P1-09", "HSTS 已启用 (SECURE_HSTS_SECONDS ≥ 31536000)",
            hsts >= 31536000,
            f"SECURE_HSTS_SECONDS={hsts}",
        )

    def _print_report(self):
        self.stdout.write("\n" + "=" * 60)
        self.stdout.write("  Health Nexus 部署就绪检查报告")
        self.stdout.write("=" * 60 + "\n")

        all_passed = True
        for priority in ["P0", "P1", "P2"]:
            checks = self.results[priority]
            if not checks:
                continue

            passed = sum(1 for c in checks if c["passed"])
            total = len(checks)
            self.stdout.write(f"\n{priority} ({passed}/{total} 通过):")
            self.stdout.write("-" * 40)

            for check in checks:
                status = "✅ PASS" if check["passed"] else "❌ FAIL"
                if check["passed"] and not self.verbose:
                    continue
                line = f"  {status} {check['id']}: {check['description']}"
                if check["detail"]:
                    line += f" ({check['detail']})"
                self.stdout.write(line)

                if not check["passed"] and priority == "P0":
                    all_passed = False

        p0_failed = sum(1 for c in self.results["P0"] if not c["passed"])
        p1_failed = sum(1 for c in self.results["P1"] if not c["passed"])

        self.stdout.write("\n" + "=" * 60)
        if p0_failed == 0:
            self.stdout.write(self.style.SUCCESS("✅ 所有 P0 检查通过，可以上线"))
        else:
            self.stdout.write(self.style.ERROR(f"❌ {p0_failed} 项 P0 检查未通过，不可上线"))

        if p1_failed > 0:
            self.stdout.write(self.style.WARNING(f"⚠️  {p1_failed} 项 P1 检查未通过，建议处理"))

        self.stdout.write("=" * 60 + "\n")
