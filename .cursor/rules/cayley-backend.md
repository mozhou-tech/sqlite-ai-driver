# Cayley 存储后端规则

## 存储后端实现
- 使用一个单独的 sqlite3 库作为 cayley 的存储后端
- 参考实现：https://github.com/cayleygraph/cayley/tree/master/storage/sqlite3
- 使用 modernc.org/sqlite 驱动（非 go-sqlite3）
- 默认关闭

## 实现要求
- 使用 modernc.org/sqlite 驱动而非 go-sqlite3
- 作为独立模块实现，不影响主驱动
- 遵循 cayley 存储后端的接口规范
- 默认情况下不启用，需要显式配置开启

