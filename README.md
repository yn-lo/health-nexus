# Health Nexus 健康宣教系统

🌐 在线演示（Demo）：https://hn.ynlo.top

[![Python](https://img.shields.io/badge/Python-3.11-blue.svg)](https://www.python.org/)
[![Django](https://img.shields.io/badge/Django-5.2-green.svg)](https://www.djangoproject.com/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-blue.svg)](https://www.postgresql.org/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

AI 驱动的智能健康宣教平台，为患者提供 7x24 小时个性化健康指导，为医护减轻重复性宣教工作量。

---

## 项目简介

Health Nexus 是一个基于 RAG（检索增强生成）技术的医疗健康宣教系统，通过 AI 大语言模型结合科室专业知识库，为患者提供准确、可追溯的健康问答服务。

### 核心特性

- **AI 智能问答**：基于 RAG 技术，AI 回答 100% 可追溯来源，零医疗幻觉
- **三端分离架构**：管理端、医护端、患者端独立工作流
- **知识库管理**：文章全生命周期管理，支持知识切片与向量化
- **患者健康档案**：完整的患者信息管理与体征数据追踪
- **角色权限控制**：基于 RBAC 的细粒度权限管理
- **系统配置管理**：三级缓存架构，支持配置热更新
- **运营数据统计**：知识覆盖率、需求热力图、用户活跃度分析

### 系统定位

> 本系统定位为**健康宣教工具**，不提供在线诊疗服务。系统无诊断权、无处方权，仅基于已审核的宣教知识库为患者提供住院期间及出院后康复相关的健康指导。

---

## 技术栈

| 层级 | 技术 | 用途 |
|------|------|------|
| 后端框架 | Django 5.2 + Django Ninja | Web 框架 + API |
| 数据库 | PostgreSQL 16 + pgvector | 关系存储 + 向量检索 |
| 缓存 | Redis 7 | 配置缓存、会话存储 |
| 异步任务 | Django-Q2 | 向量化、邮件发送 |
| 前端 | Django Templates + HTMX + Alpine.js | 服务端渲染 + 轻量交互 |
| 样式 | Tailwind CSS + DaisyUI | 原子化样式 + 组件 |
| 管理后台 | Django Unfold | 现代化 Admin 界面 |
| AI 服务 | SiliconFlow (OpenAI 兼容) | LLM + Embedding |

---

## 快速开始

### 环境要求

- Python 3.11+
- PostgreSQL 16（带 pgvector 扩展）
- Redis 7+
- Docker & Docker Compose（可选，用于容器化部署）

### 方式一：Docker 部署（推荐）

```bash
# 1. 克隆项目
git clone <repository-url>
cd health-nexus

# 2. 配置环境变量
cp .env.docker .env
# 编辑 .env 文件，修改必要配置

# 3. 一键启动
docker-compose up -d --build
```

访问地址：`http://localhost:5631`

### 方式二：本地开发

```bash
# 1. 创建虚拟环境
python -m venv venv
source venv/bin/activate  # Windows: venv\Scripts\activate

# 2. 安装依赖
pip install -r requirements.txt -r requirements-dev.txt

# 3. 配置环境变量
cp .env.example .env
# 编辑 .env 文件

# 4. 启动基础设施（PostgreSQL + Redis）
docker-compose -f docker-compose.db.yml up -d

# 5. 数据库迁移
python manage.py migrate

# 6. 创建超级用户
python manage.py createsuperuser

# 7. 收集静态文件
python manage.py collectstatic --noinput

# 8. 启动开发服务器
python manage.py runserver
```

访问地址：`http://localhost:8000`

---

## Docker 部署详解

### 配置文件说明

| 文件 | 用途 |
|------|------|
| `docker-compose.yml` | 完整应用栈部署（Web + Worker + DB + Redis） |
| `docker-compose.db.yml` | 仅基础设施（DB + Redis） |
| `Dockerfile` | 应用镜像构建配置 |
| `entrypoint.sh` | 容器启动脚本（migrate + collectstatic + createsuperuser） |
| `.env.docker` | Docker 开发环境配置模板 |
| `.env.production` | Docker 生产环境配置模板 |

### 服务架构

| 服务 | 镜像 | 用途 |
|------|------|------|
| `db` | pgvector/pgvector:pg16 | PostgreSQL 数据库 + 向量检索 |
| `redis` | redis:7-alpine | 缓存 + Django Q 消息队列 |
| `web` | 自建镜像 | Gunicorn WSGI 服务器 |
| `worker` | 自建镜像 | Django Q 异步任务执行 |

> `web` 和 `worker` 使用相同的镜像，仅启动命令不同。`web` 运行 Gunicorn，`worker` 运行 `qcluster`。

### 部署场景

#### 开发环境

```bash
cp .env.docker .env
docker-compose up -d --build
```

#### 生产环境

```bash
cp .env.production .env
# 修改所有 CHANGE-ME 配置项
# 注意：.env 文件不支持 ${VAR} 插值，密码需直接写入 URL
# 例如：Q_REDIS=redis://:your-password@redis:6379/0
docker-compose up -d --build
```

#### 仅启动基础设施

```bash
docker-compose -f docker-compose.db.yml up -d
```

### 常用命令

```bash
# 查看日志
docker-compose logs -f web

# 重启服务
docker-compose restart web

# 停止服务
docker-compose down

# 执行 Django 命令
docker-compose exec web python manage.py migrate
docker-compose exec web python manage.py createsuperuser

# 查看运行状态
docker-compose ps
```

### 数据持久化

系统使用 Docker Volumes 持久化以下数据：

| Volume | 存储内容 |
|--------|---------|
| `postgres_data` | PostgreSQL 数据库文件 |
| `redis_data` | Redis 缓存数据 |
| `static_data` | Django 静态文件 |
| `media_data` | 用户上传媒体文件 |

### 环境重置

开发或部署前需要全新环境时，可使用 `scripts/` 目录下的重置脚本：

```bash
# 本地开发：重置迁移文件 + 数据库
bash scripts/reset_migrations.sh     # Linux/macOS
.\scripts\reset_migrations.ps1       # Windows

# Docker 部署：重置容器 + 数据卷 + 迁移文件
bash scripts/reset_docker.sh         # Linux/macOS
.\scripts\reset_docker.ps1           # Windows
```

脚本会自动：
1. 清理所有应用的迁移文件
2. 删除旧数据库和缓存
3. 重新生成迁移文件
4. 执行数据库迁移（本地）或重建容器（Docker）

---

## 项目结构

```
health-nexus/
├── apps/                    # 应用模块
│   ├── base/                # 科室管理、抽象基类、品牌配置
│   ├── auth/                # 用户模型、角色权限、登录注册
│   ├── care/                # 患者档案 CRUD、体征数据
│   ├── wiki/                # 知识库管理、文章全生命周期
│   ├── chat/                # RAG 问答、会话管理
│   ├── config/              # 系统配置管理
│   └── stats/               # 运营数据统计
├── config/                  # Django 项目配置
├── docs/                    # 文档
│   └── requirements/        # 需求规格文档
├── templates/               # HTML 模板
├── static/                  # 静态资源
├── media/                   # 媒体文件
├── paper/                   # 知识种子数据
├── docker-compose.yml       # Docker Compose 配置
├── docker-compose.db.yml    # 基础设施配置
├── Dockerfile               # Docker 镜像构建
├── entrypoint.sh            # 容器启动脚本
├── scripts/                 # 运维脚本
│   ├── reset_migrations.sh  # 重置迁移文件（Unix）
│   ├── reset_migrations.ps1 # 重置迁移文件（Windows）
│   ├── reset_docker.sh      # 重置Docker环境（Unix）
│   └── reset_docker.ps1     # 重置Docker环境（Windows）
├── .env.example             # 环境变量模板
├── .env.docker              # Docker 开发环境配置
├── .env.production          # Docker 生产环境配置
├── requirements.txt         # Python 生产依赖
├── requirements-dev.txt     # Python 开发/测试依赖
└── manage.py                # Django 管理命令
```

---

## 架构设计

### 分层架构

```
┌──────────────────────────────────────────────────┐
│  表现层    Django Templates + HTMX + Alpine.js    │
│            Django Ninja API                       │
├──────────────────────────────────────────────────┤
│  视图层    FBV/CBV → 调用服务层 → 渲染/返回JSON   │
├──────────────────────────────────────────────────┤
│  服务层    业务逻辑的唯一承载层                    │
│            视图层禁止跨过服务层直接操作ORM         │
├──────────────────────────────────────────────────┤
│  数据层    Django ORM + pgvector + 字段加密       │
├──────────────────────────────────────────────────┤
│  基础设施  PostgreSQL / Redis / Django-Q2 / AI API│
└──────────────────────────────────────────────────┘
```

### 三端分离

| 端 | 面向用户 | 路由入口 | 功能范围 |
|----|---------|---------|---------|
| 管理端 | 超级管理员、科室管理员 | `/admin/login/` | 用户管理、科室管理、系统配置 |
| 医护端 | 医生、护士、科室管理员 | `/accounts/staff-login/` | 患者管理、文章发布与审核、数据统计 |
| 患者端 | 患者 | `/accounts/patient-login/` | 健康档案、AI问诊、知识浏览、消息评价 |

### 依赖拓扑

```
base (01)
  └── auth (02)
        ├── care (03) ──┐
        └── wiki (04) ──┼──→ chat (05) ──→ stats (07)
config (06) ────────────┘
```

---

## 配置说明

### 环境变量

系统通过 `.env` 文件管理配置，支持以下环境文件：

| 文件 | 环境 | 说明 |
|------|------|------|
| `.env.example` | 模板 | 配置参考，不可直接使用 |
| `.env` | 当前环境 | 实际生效的配置 |
| `.env.docker` | Docker 开发 | Docker 开发环境配置 |
| `.env.production` | Docker 生产 | Docker 生产环境配置 |

### 必要配置项

```bash
# Django 核心
SECRET_KEY=<your-secret-key>
DEBUG=False

# 数据库
DB_NAME=health_nexus
DB_USER=postgres
DB_PASSWORD=<your-db-password>
DB_HOST=db              # Docker环境使用 db，本地使用 localhost
DB_PORT=5432

# Redis（需与 docker-compose.yml 中 redis --requirepass 一致）
REDIS_PASSWORD=<your-redis-password>
Q_REDIS=redis://:<your-redis-password>@redis:6379/0
CACHE_REDIS=redis://:<your-redis-password>@redis:6379/1

# 字段加密密钥
FIELD_ENCRYPTION_KEY=<your-fernet-key>
```

> **AI 配置已迁移到数据库管理**，通过 Django Admin > 系统配置 > AI 提供商配置 管理，无需在环境变量中设置 `AI_*` 相关配置。

### 生成密钥

```bash
# Django Secret Key
python -c 'from django.core.management.utils import get_random_secret_key; print(get_random_secret_key())'

# 字段加密密钥
python -c 'from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())'
```

---

## 开发与测试

### 运行测试

```bash
# 运行所有测试
pytest

# 运行特定模块测试
pytest apps/chat/tests/

# 运行测试并生成覆盖率报告
pytest --cov=apps --cov-report=html

# 运行 E2E 测试
pytest --browser chromium
```

### 代码规范

- 服务层统一承载业务逻辑
- 视图层禁止跨服务层直接操作 ORM
- 所有公开 API 必须有文档字符串
- 新功能必须附带测试

---

## 文档

- [需求规格总览](docs/REQUIREMENTS.md)
- [前端开发规范](docs/FRONTEND_DEV_SPEC.md)
- [领域需求文档](docs/requirements/)

---

## 未来规划

| 版本 | 功能 | 描述 |
|------|------|------|
| v1.3 | 语音识别 | 支持患者语音提问 |
| v1.3 | 图片识别 | 药品包装盒照片识别 |
| v1.4 | 随访计划 Bot | 基于患者手术/出院日期自动推送宣教内容 |
| v1.4 | 主动式关怀 | 术后定时推送护理知识与复查提醒 |
| v2.0 | HIS 深度融合 | 自动读取检验报告并 AI 解读 |
| v2.0 | 检查报告解读 | 自动读取影像报告并 AI 解读 |

---

## 许可证

MIT License

---

**Health Nexus** - 让健康知识触手可及
