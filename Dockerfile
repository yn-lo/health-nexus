# Health Nexus 全栈构建（后端 + 前端静态文件）
# 用法: docker build -t health-nexus .
# 前端在 web-builder 阶段用 node:22-alpine 完成 npm ci + npm run build

# ---- 前端构建阶段 ----
FROM node:22-alpine AS web-builder
ENV NPM_CONFIG_REGISTRY=https://registry.npmmirror.com
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# ---- 后端构建阶段 ----
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

COPY backend/ .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /bin/worker ./cmd/worker

# ---- 运行阶段 ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/server /bin/server
COPY --from=builder /bin/worker /bin/worker

WORKDIR /app
COPY backend/config.yaml ./config.yaml
COPY backend/migrations ./migrations
COPY --from=web-builder /web/dist ./web

ENV TZ=Asia/Shanghai

# 默认启动 server；docker-compose 中 worker 服务覆盖 command
CMD ["/bin/server"]
