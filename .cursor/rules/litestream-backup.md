# Litestream 备份恢复规则

## 备份恢复实现
- 使用 litestream 实现 sqlite3 的备份和恢复
- 参考文档：https://litestream.io/guides/go-library/
- 使用 modernc.org/sqlite 驱动（非 go-sqlite3）
- 默认关闭

## 实现要求
- 使用 modernc.org/sqlite 驱动而非 go-sqlite3
- 遵循 litestream Go 库的使用规范
- 提供数据库备份和恢复功能
- 默认情况下不启用，需要显式配置开启

