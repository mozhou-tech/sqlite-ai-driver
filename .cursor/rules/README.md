# SQLite AI Driver 编程规则

本目录包含多个独立的编程规则文件，每个文件专注于特定的功能模块：

1. **sqlite3-driver.md** - SQLite3 驱动基础封装和 WAL 模式规则
2. **sqlite3-vec.md** - SQLite3-Vec 向量扩展规则
3. **fts5.md** - FTS5 全文索引扩展规则
4. **cayley-backend.md** - Cayley 图数据库存储后端规则
5. **litestream-backup.md** - Litestream 备份恢复规则

## 驱动说明
- 主驱动基于 `github.com/mattn/go-sqlite3`，默认开启 WAL
- 扩展功能（vec、fts5）基于 go-sqlite3 驱动，默认开启
- 独立功能（cayley、litestream）使用 `modernc.org/sqlite` 驱动，默认关闭
