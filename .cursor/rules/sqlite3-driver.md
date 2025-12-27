# SQLite3 驱动基础规则

## 驱动封装
- 本工程是基于 `github.com/mattn/go-sqlite3` 的二次封装
- 默认开启 WAL（Write-Ahead Logging）模式

## 实现要求
- 所有基于 go-sqlite3 驱动的功能都应遵循此基础封装规范
- WAL 模式应在驱动初始化时默认启用

