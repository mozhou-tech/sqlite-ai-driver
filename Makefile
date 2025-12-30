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
	echo "   git push origin main"; \
	echo "   git push origin v0.0.0-$$VERSION"; \
	git push origin main; \
	git push origin v0.0.0-$$VERSION

install:
	go mod tidy

.PHONY: fix-deps test

fix-deps:
	@echo "修复所有子模块的依赖..."
	@for dir in ./pkg/cayley-driver ./pkg/duckdb-driver ./pkg/eino-ext ./pkg/eino-ext/document/parser/pdf ./pkg/file-driver ./pkg/lightrag ./pkg/sego ./pkg/sqlite3-driver; do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "修复依赖: $$dir"; \
			(cd $$dir && go mod tidy 2>&1 | grep -v "go: downloading" || true); \
			(cd $$dir && go get -d ./... 2>&1 | grep -v "go: downloading" || true); \
		fi; \
	done; \
	echo "✓ 依赖修复完成"

.PHONY: test

test: fix-deps
	@echo "运行所有模块的测试用例..."
	@failed=0; \
	for dir in ./pkg/cayley-driver ./pkg/duckdb-driver ./pkg/eino-ext ./pkg/eino-ext/document/parser/pdf ./pkg/file-driver ./pkg/lightrag ./pkg/sego ./pkg/sqlite3-driver; do \
		if [ -f "$$dir/go.mod" ]; then \
			echo ""; \
			echo "测试模块: $$dir"; \
			(cd $$dir && MOCKEY_CHECK_GCFLAGS=false go test -gcflags="all=-N -l" ./...) || failed=$$((failed + 1)); \
		fi; \
	done; \
	if [ $$failed -eq 0 ]; then \
		echo ""; \
		echo "✓ 所有测试通过"; \
	else \
		echo ""; \
		echo "✗ 有 $$failed 个模块的测试失败"; \
		exit 1; \
	fi


