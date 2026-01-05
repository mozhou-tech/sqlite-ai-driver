# SQLite3 Driver for GORM

这是一个为 GORM 提供的 SQLite3 dialector 实现，使用自定义的 `sqlite3-driver` 驱动。

## 特性

- 使用自定义的 `sqlite3` 驱动（支持自动路径处理和 WAL 模式）
- 完全兼容 GORM 的所有功能
- 支持自动迁移、查询、事务等所有 GORM 特性
- 支持 `workingDir` 参数，自动处理数据库路径

## 使用方法

### 基本用法

```go
import (
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver" // 导入以注册驱动
    sqlite3driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
    "gorm.io/gorm"
)

// 打开数据库连接
db, err := gorm.Open(sqlite3driver.Open("database.db"), &gorm.Config{})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### 使用 workingDir 参数

```go
// 使用 workingDir 参数，数据库会自动创建在 {workingDir}/db/database.db
dsn := "database.db?workingDir=/path/to/working/dir"
db, err := gorm.Open(sqlite3driver.Open(dsn), &gorm.Config{})
```

### 完整示例

```go
package main

import (
    "log"
    
    _ "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
    sqlite3driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/sqlite3-driver"
    "gorm.io/gorm"
)

type User struct {
    ID   uint   `gorm:"primaryKey"`
    Name string `gorm:"type:varchar(100)"`
}

func main() {
    // 打开数据库
    db, err := gorm.Open(sqlite3driver.Open("test.db"), &gorm.Config{})
    if err != nil {
        log.Fatal(err)
    }
    
    // 自动迁移
    db.AutoMigrate(&User{})
    
    // 创建用户
    user := User{Name: "张三"}
    db.Create(&user)
    
    // 查询用户
    var foundUser User
    db.First(&foundUser, user.ID)
    log.Printf("Found user: %+v", foundUser)
}
```

## API 参考

### `Open(dsn string) gorm.Dialector`

创建一个新的 SQLite3 dialector。

**参数：**
- `dsn`: 数据库连接字符串，支持相对路径和绝对路径。如果提供了 `workingDir` 参数（通过查询字符串），会使用 sqlite3-driver 的路径处理功能。

**返回值：**
- `gorm.Dialector`: GORM dialector 实例

## 与 gorm.io/driver/sqlite 的区别

- 使用自定义的 `sqlite3` 驱动，而不是 `modernc.org/sqlite`
- 支持自动路径处理（通过 `workingDir` 参数）
- 自动设置 WAL 模式
- 自动创建必要的目录

## 注意事项

- SQLite 建议使用单连接，dialector 会自动设置连接池参数（MaxIdleConns=1, MaxOpenConns=1）
- 数据库文件会自动创建在指定位置
- 如果使用相对路径且未提供 `workingDir`，数据库会创建在 `./data/db/` 目录下

