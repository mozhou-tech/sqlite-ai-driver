# Attachments 附件管理模块

附件管理模块提供了文件的存储、删除、查询等功能，文件按日期自动组织到子目录中。

## 功能特性

1. 在 `WorkingDir` 目录下自动创建 `attachments` 目录
2. 文件按日期创建子目录（格式：`YYYYMMDD`）
3. 提供文件的存入、删除、查看基本信息、获取绝对路径等功能
4. 使用 SQLite 数据库存储文件元数据
5. 自动计算文件 MD5 值
6. 支持 MIME 类型和扩展元数据

## 接口说明

### Manager 结构体

附件管理器，管理所有文件操作。

```go
type Manager struct {
    // 内部字段，不直接访问
}
```

### FileInfo 结构体

文件基本信息。

```go
type FileInfo struct {
    ID           string                 // 文件ID（主键，格式：YYYYMMDD/filename）
    Name         string                 // 文件名
    Size         int64                  // 文件大小（字节）
    ModTime      time.Time              // 修改时间
    DateDir      string                 // 日期目录（YYYYMMDD）
    RelativePath string                 // 相对路径（相对于attachments目录）
    AbsolutePath string                 // 绝对路径
    MimeType     string                 // MIME类型
    MD5          string                 // 文件MD5值
    Metadata     map[string]interface{} // 扩展元数据（JSON）
    CreatedAt    time.Time              // 创建时间
    UpdatedAt    time.Time              // 更新时间
}
```

### 核心接口

#### New - 创建附件管理器

```go
func New(workingDir string) (*Manager, error)
```

创建附件管理器实例。

- **参数**：
  - `workingDir`: 工作目录，`attachments` 目录将创建在此目录下
- **返回**：管理器实例和错误信息

#### Close - 关闭管理器

```go
func (m *Manager) Close() error
```

关闭管理器，释放数据库连接等资源。

#### Store - 存储文件

```go
func (m *Manager) Store(filename string, data []byte) (string, error)
```

存储文件内容。

- **参数**：
  - `filename`: 文件名（不包含路径）
  - `data`: 文件内容
- **返回**：文件ID（相对路径，格式：`YYYYMMDD/filename`）和错误信息

#### StoreWithMetadata - 存储文件并保存元数据

```go
func (m *Manager) StoreWithMetadata(filename string, data []byte, mimeType *string, metadata map[string]interface{}) (string, error)
```

存储文件并保存 MIME 类型和扩展元数据。

- **参数**：
  - `filename`: 文件名（不包含路径）
  - `data`: 文件内容
  - `mimeType`: MIME类型（可选，传 `nil` 表示不设置）
  - `metadata`: 扩展元数据（可选，传 `nil` 表示不设置）
- **返回**：文件ID和错误信息

#### StoreFromFile - 从文件路径存储文件

```go
func (m *Manager) StoreFromFile(filePath string) (string, error)
```

从本地文件路径复制文件到附件目录。

- **参数**：
  - `filePath`: 源文件路径
- **返回**：文件ID和错误信息

#### StoreFromFileWithMetadata - 从文件路径存储文件并保存元数据

```go
func (m *Manager) StoreFromFileWithMetadata(filePath string, mimeType *string, metadata map[string]interface{}) (string, error)
```

从本地文件路径复制文件到附件目录，并保存元数据。

- **参数**：
  - `filePath`: 源文件路径
  - `mimeType`: MIME类型（可选）
  - `metadata`: 扩展元数据（可选）
- **返回**：文件ID和错误信息

#### Delete - 删除文件

```go
func (m *Manager) Delete(fileID string) error
```

删除指定文件及其元数据。

- **参数**：
  - `fileID`: 文件ID（相对路径，格式：`YYYYMMDD/filename`）
- **返回**：错误信息

#### GetInfo - 获取文件基本信息

```go
func (m *Manager) GetInfo(fileID string) (*FileInfo, error)
```

获取文件基本信息。优先从数据库读取，如果不存在则从文件系统读取。

- **参数**：
  - `fileID`: 文件ID（相对路径）
- **返回**：文件信息结构体和错误信息

#### GetAbsolutePath - 获取文件的绝对路径

```go
func (m *Manager) GetAbsolutePath(fileID string) (string, error)
```

获取文件的绝对路径。

- **参数**：
  - `fileID`: 文件ID（相对路径）
- **返回**：绝对路径和错误信息

#### Read - 读取文件内容

```go
func (m *Manager) Read(fileID string) ([]byte, error)
```

读取文件内容。

- **参数**：
  - `fileID`: 文件ID（相对路径）
- **返回**：文件内容和错误信息

#### List - 列出文件ID

```go
func (m *Manager) List(dateDir string) ([]string, error)
```

列出指定日期目录下的所有文件ID（从文件系统读取）。

- **参数**：
  - `dateDir`: 日期目录（格式：`YYYYMMDD`），如果为空则列出所有日期目录
- **返回**：文件ID列表和错误信息

#### ListAll - 列出所有文件信息

```go
func (m *Manager) ListAll(dateDir string) ([]*FileInfo, error)
```

列出指定日期目录下的所有文件信息（从文件系统读取）。

- **参数**：
  - `dateDir`: 日期目录（格式：`YYYYMMDD`），如果为空则列出所有日期目录
- **返回**：文件信息列表和错误信息

#### GetBaseDir - 获取基础目录

```go
func (m *Manager) GetBaseDir() string
```

获取 `attachments` 基础目录的绝对路径。

- **返回**：基础目录的绝对路径

## 使用示例

```go
package main

import (
    "fmt"
    "log"
    "path/filepath"
    
    "github.com/mozhou-tech/sqlite-ai-driver/pkg/attachments"
)

func main() {
    // 创建管理器
    workingDir := "./data"
    mgr, err := attachments.New(workingDir)
    if err != nil {
        log.Fatal(err)
    }
    defer mgr.Close()
    
    // 存储文件
    filename := "test.txt"
    data := []byte("Hello, World!")
    fileID, err := mgr.Store(filename, data)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("文件已存储，ID: %s\n", fileID)
    
    // 获取文件信息
    info, err := mgr.GetInfo(fileID)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("文件名: %s, 大小: %d, MD5: %s\n", info.Name, info.Size, info.MD5)
    
    // 获取绝对路径
    absPath, err := mgr.GetAbsolutePath(fileID)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("绝对路径: %s\n", absPath)
    
    // 读取文件
    content, err := mgr.Read(fileID)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("文件内容: %s\n", string(content))
    
    // 列出所有文件
    files, err := mgr.List("")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("文件数量: %d\n", len(files))
    
    // 删除文件
    if err := mgr.Delete(fileID); err != nil {
        log.Fatal(err)
    }
    fmt.Println("文件已删除")
}
```

## 注意事项

1. 文件ID格式为 `YYYYMMDD/filename`，其中日期目录自动创建
2. 如果同名文件已存在，会自动添加时间戳后缀避免覆盖
3. 文件元数据存储在 SQLite 数据库中，数据库文件名为 `attachments.db`
4. 删除文件时会同时删除文件系统中的文件和数据库中的元数据
5. `GetInfo` 方法优先从数据库读取，如果数据库中没有记录，则从文件系统读取基本信息