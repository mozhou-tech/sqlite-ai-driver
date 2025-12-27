# SQLite 插件编译 Makefile
# 用于编译 cgo 目录下的 SQLite 扩展插件

# 输出目录
OUTPUT_DIR := ../exts
FTS5_DIR := fts5
VEC_DIR := sqlite-vec

# 检测操作系统
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
	SHARED_EXT := .so
	LDFLAGS := -shared -fPIC
endif
ifeq ($(UNAME_S),Darwin)
	SHARED_EXT := .dylib
	LDFLAGS := -dynamiclib -fPIC
endif
ifeq ($(OS),Windows_NT)
	SHARED_EXT := .dll
	LDFLAGS := -shared
endif

# 编译器标志
CFLAGS := -O2 -fPIC -Wall
# 注意：不使用 SQLITE_CORE，以便编译为可加载的扩展插件

# 默认目标
.PHONY: all clean fts5 vec

all: fts5 vec
	@echo "✓ 所有插件编译完成"

# 创建输出目录
$(OUTPUT_DIR):
	@mkdir -p $(OUTPUT_DIR)

# 编译 fts5 插件
fts5: $(OUTPUT_DIR)/fts5$(SHARED_EXT)
	@echo "✓ fts5 插件编译完成"

$(OUTPUT_DIR)/fts5$(SHARED_EXT): $(FTS5_DIR)/fts5.c $(FTS5_DIR)/fts5.h $(FTS5_DIR)/sqlite3.h $(FTS5_DIR)/sqlite3ext.h | $(OUTPUT_DIR)
	@echo "编译 fts5 插件..."
	@# fts5 需要作为扩展编译（不使用 SQLITE_CORE）
	$(CC) $(CFLAGS) $(LDFLAGS) \
		-I$(FTS5_DIR) \
		-o $@ \
		$(FTS5_DIR)/fts5.c \
		-lm
	@echo "✓ fts5 插件已生成: $@"

# 编译 sqlite-vec 插件
vec: $(OUTPUT_DIR)/sqlite-vec$(SHARED_EXT)
	@echo "✓ sqlite-vec 插件编译完成"

$(OUTPUT_DIR)/sqlite-vec$(SHARED_EXT): $(VEC_DIR)/sqlite-vec.c $(VEC_DIR)/sqlite-vec.h | $(OUTPUT_DIR)
	@echo "编译 sqlite-vec 插件..."
	@# 需要找到 sqlite3.h 和 sqlite3ext.h 的位置
	@SQLITE3_H_PATH=$$(find $(FTS5_DIR) -name "sqlite3.h" | head -1); \
	SQLITE3EXT_H_PATH=$$(find $(FTS5_DIR) -name "sqlite3ext.h" | head -1); \
	if [ -z "$$SQLITE3_H_PATH" ]; then \
		echo "错误: 找不到 sqlite3.h"; \
		exit 1; \
	fi; \
	if [ -z "$$SQLITE3EXT_H_PATH" ]; then \
		echo "错误: 找不到 sqlite3ext.h"; \
		exit 1; \
	fi; \
	SQLITE3_DIR=$$(dirname $$SQLITE3_H_PATH); \
	$(CC) $(CFLAGS) $(LDFLAGS) \
		-I$(VEC_DIR) \
		-I$$SQLITE3_DIR \
		-o $@ \
		$(VEC_DIR)/sqlite-vec.c \
		-lm
	@echo "✓ sqlite-vec 插件已生成: $@"

# 清理编译产物
clean:
	@echo "清理编译产物..."
	rm -f $(OUTPUT_DIR)/fts5$(SHARED_EXT)
	rm -f $(OUTPUT_DIR)/sqlite-vec$(SHARED_EXT)
	@echo "✓ 清理完成"

# 显示帮助信息
help:
	@echo "SQLite 插件编译 Makefile"
	@echo ""
	@echo "用法:"
	@echo "  make          - 编译所有插件"
	@echo "  make fts5     - 仅编译 fts5 插件"
	@echo "  make vec      - 仅编译 sqlite-vec 插件"
	@echo "  make clean    - 清理编译产物"
	@echo "  make help     - 显示此帮助信息"
	@echo ""
	@echo "输出目录: $(OUTPUT_DIR)"
	@echo "操作系统: $(UNAME_S)"
	@echo "共享库扩展: $(SHARED_EXT)"

