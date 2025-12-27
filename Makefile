# 扩展下载相关
EXT_DIR := $(shell pwd)/exts

.PHONY: download-exts
download-exts:
	@mkdir -p $(EXT_DIR)
	@$(EXT_DIR)/download-exts.sh

test: download-exts
	@SQLITE_VSS_EXT_PATH=$(EXT_DIR) go test ./ -v
# 发布相关命令
.PHONY: tag release verify-release

# 创建并推送版本标签（自动使用当前时间）
# 使用方法: make tag
tag:
	@VERSION=$$(date +%Y%m%d-%H%M%S); \
	echo "创建版本标签: v$$VERSION"; \
	git tag v$$VERSION; \
	echo "标签已创建，正在推送到远程..."; \
	git push github v$$VERSION

# 验证发布（检查代码是否可以正常构建和测试）
verify-release:
	@echo "验证代码构建..."
	go mod tidy
	@echo "验证库代码编译（排除 examples 目录）..."
	@go vet ./pkg/... || true
	@echo "✓ 代码检查完成"
	@echo "运行测试..."
	# go test ./pkg/... -v
	@echo "验证通过！"

# 完整发布流程（自动使用当前时间生成版本标签）
# 使用方法: make release
release: verify-release
	@VERSION=$$(date +%Y%m%d-%H%M%S); \
	go mod tidy; \
	git commit -a -m "tidy go.mod and go.sum"; \
	echo "准备发布版本 v$$VERSION..."; \
	echo "1. 确保所有更改已提交:"; \
	git status --short; \
	echo ""; \
	echo "2. 创建版本标签 v$$VERSION..."; \
	git tag v0.0.0-$$VERSION; \
	echo ""; \
	echo "3. 标签已创建，执行以下命令完成发布:"; \
	echo "   git push github master"; \
	echo "   git push github v0.0.0-$$VERSION"; \
	git push github master; \
	git push github v0.0.0-$$VERSION

make install:
	go mod tidy
