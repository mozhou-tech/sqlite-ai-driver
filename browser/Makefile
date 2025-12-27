.PHONY: install dev build api frontend seed clean-lock stop force-clean

# 安装前端依赖
install:
	pnpm install
	cd api && go mod tidy


# 启动开发服务器（前端和后端）
dev: stop
	@echo "启动后端 API 服务器（使用 air 热重载）..."
	@cd api && air &
	@sleep 2
	@echo "启动前端开发服务器..."
	@pnpm dev

# 仅启动后端
api:
	cd api && go run main.go

# 仅启动前端
frontend:
	pnpm dev

# 构建前端
build:
	pnpm build

# 构建后端
build-api:
	cd api && go build -o browser-api main.go

# 生成示例数据
seed:
	@echo "🌱 生成示例数据..."
	@cd api && go run seed.go

# 生成大量数据用于性能测试（1万条）
largeseed:
	@echo "🚀 生成大量数据用于性能测试（1万条）..."
	@cd api && go run largeseed.go

# 停止所有相关进程
stop:
	@echo "🛑 停止后端服务..."
	@-lsof -ti :40111 | xargs kill -9 2>/dev/null || true
	@-pkill -f "air" || true
	@-pkill -f "tmp/main" || true
	@-pkill -f "go run.*main.go" || true
	@sleep 1
	@echo "✅ 后端服务已停止"

# 清理数据库锁文件（当遇到 "Another process is using this Badger database" 错误时使用）
clean-lock:
	@echo "🧹 清理数据库锁文件..."
	@rm -f api/data/browser-db/LOCK
	@rm -f api/data/browser-db/graph/LOCK 2>/dev/null || true
	@echo "✅ 锁文件已清理"

# 强制清理：停止所有相关进程并清理锁文件
force-clean: stop
	@echo "✅ 强制清理完成"

test:
	cd api &&  go mod tidy && go test ./...

build-api:
	@echo "🏗️  构建后端服务 (不包含 Swagger)..."
	@cd api && go mod tidy && go build -ldflags="-s -w" -o browser-api .
	@gsa api/browser-api

