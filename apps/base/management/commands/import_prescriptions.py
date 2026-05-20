import os
import json
import markdown

from django.contrib.auth import get_user_model
from django.core.management.base import BaseCommand
from django.db import transaction

from apps.base.models import Department
from apps.wiki.models import Article

User = get_user_model()

DEPARTMENTS = [
    {
        "name": "慢病与地方病科",
        "tenant_code": "chronic",
        "description": "覆盖高血压、脑血管病、冠心病、糖尿病、慢阻肺、肺结核、尿毒症、类风湿关节炎、骨关节炎及地方病等慢性病与地方病的健康宣教",
        "is_public": False,
        "prescriptions": [
            "1.高血压患者健康教育处方.md",
            "2.脑血管病患者健康教育处方.md",
            "3.冠心病患者健康教育处方.md",
            "4.2型糖尿病患者健康教育处方.md",
            "9.慢阻肺患者健康教育处方.md",
            "10.重型老年慢性支气管炎患者健康教育处方.md",
            "11.尿毒症患者健康教育处方.md",
            "12.类风湿关节炎患者健康教育处方.md",
            "13.骨关节炎患者健康教育处方.md",
            "14.肺结核患者健康教育处方.md",
            "15.血吸虫病患者健康教育处方.md",
            "16.包虫病患者健康教育处方.md",
            "17.克山病患者健康教育处方.md",
            "18.大骨节病患者健康教育处方.md",
            "19.碘缺乏病患者健康教育处方.md",
            "20.燃煤污染型地方性氟中毒患者健康教育处方.md",
            "21.饮茶型地方性氟中毒患者健康教育处方.md",
            "22.饮水型地方性氟中毒患者健康教育处方.md",
            "23.地方性砷中毒患者健康教育处方.md",
        ],
    },
    {
        "name": "妇幼与青少年健康科",
        "tenant_code": "mch",
        "description": "覆盖乳腺癌、宫颈癌、妇科感染、孕期贫血、孕产期抑郁、儿童先心病、儿童白血病、儿童癫痫、儿童营养不良、儿童肥胖、儿童贫血、儿童肺炎、儿童腹泻、儿童龋病、青少年肥胖、儿童近视、青少年抑郁症等妇幼与青少年健康的健康宣教",
        "is_public": False,
        "prescriptions": [
            "24.乳腺癌患者健康教育处方.md",
            "27.外阴阴道假丝酵母菌病患者健康教育处方.md",
            "28.细菌性阴道病患者健康教育处方.md",
            "29.滴虫阴道炎患者健康教育处方.md",
            "30.急性宫颈炎患者健康教育处方.md",
            "31.盆腔炎性疾病患者健康教育处方.md",
            "32.孕期贫血患者健康教育处方.md",
            "33.孕产期抑郁患者健康教育处方.md",
            "34.儿童先天性心脏病患者健康教育处方.md",
            "35.儿童急性白血病患者健康教育处方.md",
            "36.儿童癫痫患者健康教育处方.md",
            "37.5岁以下儿童营养不良患者健康教育处方.md",
            "38.学龄前儿童肥胖患者健康教育处方.md",
            "39.儿童缺铁性贫血患者健康教育处方.md",
            "40.儿童肺炎患者健康教育处方.md",
            "41.儿童腹泻病患者健康教育处方.md",
            "42.儿童龋病患者健康教育处方.md",
            "43.青少年肥胖患者健康教育处方.md",
            "44.儿童青少年近视患者健康教育处方.md",
            "45.青少年抑郁症患者健康教育处方.md",
        ],
    },
    {
        "name": "肿瘤防治科",
        "tenant_code": "public_oncology",
        "description": "覆盖肺癌、食管癌、胃癌、结直肠癌、乳腺癌、宫颈癌、宫颈癌前病变等肿瘤防治的公共健康宣教，对所有科室可见",
        "is_public": True,
        "prescriptions": [
            "5.肺癌患者健康教育处方.md",
            "6.食管癌患者健康教育处方.md",
            "7.胃癌患者健康教育处方.md",
            "8.结直肠癌患者健康教育处方.md",
            "24.乳腺癌患者健康教育处方.md",
            "25.宫颈癌患者健康教育处方.md",
            "26.宫颈癌前病变患者健康教育处方.md",
        ],
    },
]


class Command(BaseCommand):
    help = "导入国家卫健委45种疾病健康教育处方到3个知识域科室"

    def add_arguments(self, parser):
        parser.add_argument(
            "--seed-dir",
            type=str,
            default=None,
            help="处方文件目录（默认为 paper/seed_data/nhc_prescriptions）",
        )
        parser.add_argument(
            "--publish",
            action="store_true",
            default=False,
            help="导入后直接发布文章（默认为草稿状态）",
        )
        parser.add_argument(
            "--force",
            action="store_true",
            default=False,
            help="强制重新导入（删除已有同名文章后重新创建）",
        )

    def handle(self, *args, **options):
        seed_dir = options["seed_dir"]
        publish = options["publish"]
        force = options["force"]

        if not seed_dir:
            base_dir = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
            seed_dir = os.path.join(base_dir, "paper", "seed_data", "nhc_prescriptions")

        if not os.path.isdir(seed_dir):
            self.stderr.write(self.style.ERROR(f"处方目录不存在: {seed_dir}"))
            return

        admin_user = User.objects.filter(is_superuser=True).first()
        if not admin_user:
            self.stderr.write(self.style.ERROR("未找到超级用户，请先创建超级用户"))
            return

        self.stdout.write(f"使用作者: {admin_user.username}")
        self.stdout.write(f"处方目录: {seed_dir}")

        total_created = 0
        total_skipped = 0

        for dept_config in DEPARTMENTS:
            self.stdout.write(self.style.NOTICE(f"\n=== 处理科室: {dept_config['name']} ==="))

            dept, dept_created = Department.objects.get_or_create(
                name=dept_config["name"],
                defaults={
                    "tenant_code": dept_config["tenant_code"],
                    "description": dept_config["description"],
                    "is_public": dept_config["is_public"],
                },
            )
            if dept_created:
                self.stdout.write(self.style.SUCCESS(f"  创建科室: {dept.name} (is_public={dept.is_public})"))
            else:
                self.stdout.write(f"  科室已存在: {dept.name} (id={dept.id})")

            for filename in dept_config["prescriptions"]:
                filepath = os.path.join(seed_dir, filename)
                if not os.path.isfile(filepath):
                    self.stderr.write(self.style.WARNING(f"  文件不存在: {filename}"))
                    continue

                title = filename.replace(".md", "").lstrip("0123456789.")

                if force:
                    deleted, _ = Article.objects.filter(title=title, department=dept).delete()
                    if deleted:
                        self.stdout.write(f"  删除旧文章: {title} (删除{deleted}条)")

                if Article.objects.filter(title=title, department=dept).exists():
                    self.stdout.write(f"  跳过（已存在）: {title}")
                    total_skipped += 1
                    continue

                with open(filepath, "r", encoding="utf-8") as f:
                    md_content = f.read().strip()

                if not md_content:
                    self.stdout.write(self.style.WARNING(f"  跳过（内容为空）: {title}"))
                    continue

                html_content = markdown.markdown(md_content, extensions=["tables", "fenced_code"])
                quill_json = json.dumps({
                    "delta": {"ops": [{"insert": md_content + "\n"}]},
                    "html": html_content,
                    "plain": md_content,
                }, ensure_ascii=False)

                status = Article.Status.PUBLISHED if publish else Article.Status.DRAFT

                article = Article.objects.create(
                    title=title,
                    content=quill_json,
                    author=admin_user,
                    department=dept,
                    status=status,
                    source_type="AI_IMPORT",
                )
                self.stdout.write(self.style.SUCCESS(f"  创建文章: {title} [{status}] (id={article.id})"))
                total_created += 1

        self.stdout.write(self.style.SUCCESS(f"\n导入完成: 创建{total_created}篇, 跳过{total_skipped}篇"))
