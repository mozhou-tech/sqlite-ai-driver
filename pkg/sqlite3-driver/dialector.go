package sqlite3_driver

import (
	"database/sql"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// Dialector 是 GORM 的 SQLite3 dialector 实现
// 它使用注册的 "sqlite3" 驱动来打开数据库连接
type Dialector struct {
	DSN string
}

// Name 返回 dialector 的名称
func (d Dialector) Name() string {
	return "sqlite3"
}

// Initialize 初始化数据库连接
func (d Dialector) Initialize(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if d.DSN == "" {
		return fmt.Errorf("DSN is required")
	}

	// 使用注册的 "sqlite3" 驱动打开数据库连接
	sqlDB, err := sql.Open("sqlite3", d.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数（SQLite 建议使用单连接）
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(0)

	// 设置 dialector 和连接池
	// 注意：需要先设置 Dialector，因为 GORM 内部可能会使用它
	db.Dialector = d

	// 直接使用 sql.DB，GORM 会自动处理
	// ConnPool 是一个接口，*sql.DB 实现了该接口
	db.ConnPool = sqlDB

	return nil
}

// Migrator 返回迁移器
func (d Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return &Migrator{
		Migrator: migrator.Migrator{
			Config: migrator.Config{
				DB:                          db,
				Dialector:                   d,
				CreateIndexAfterCreateTable: true,
			},
		},
	}
}

// DataTypeOf 返回字段的 SQLite 数据类型
func (d Dialector) DataTypeOf(field *schema.Field) string {
	switch field.DataType {
	case schema.Bool:
		return "numeric"
	case schema.Int, schema.Uint:
		if field.Size <= 8 {
			return "integer"
		}
		return "bigint"
	case schema.Float:
		return "real"
	case schema.String:
		if field.Size > 0 && field.Size < 65532 {
			return fmt.Sprintf("varchar(%d)", field.Size)
		}
		return "text"
	case schema.Time:
		return "datetime"
	case schema.Bytes:
		return "blob"
	default:
		return "text"
	}
}

// DefaultValueOf 返回字段的默认值表达式
func (d Dialector) DefaultValueOf(field *schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}

// BindVarTo 将变量绑定到 SQL 语句中
func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	writer.WriteByte('?')
}

// QuoteTo 为标识符添加引号
func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	writer.WriteString(`"`)
	writer.WriteString(str)
	writer.WriteString(`"`)
}

// Explain 格式化 SQL 语句，用于调试
func (d Dialector) Explain(sql string, vars ...interface{}) string {
	// 简单的 SQL 格式化，将变量替换为占位符
	result := sql
	for i := range vars {
		result = fmt.Sprintf("%s [%v]", result, vars[i])
	}
	return result
}

// Migrator 是 SQLite3 的迁移器实现
type Migrator struct {
	migrator.Migrator
}

// AutoMigrate 自动迁移表结构
func (m Migrator) AutoMigrate(values ...interface{}) error {
	return m.Migrator.AutoMigrate(values...)
}

// CurrentDatabase 返回当前数据库名称
func (m Migrator) CurrentDatabase() string {
	return "main"
}

// FullDataTypeOf 返回完整的字段数据类型定义
func (m Migrator) FullDataTypeOf(field *schema.Field) clause.Expr {
	expr := clause.Expr{SQL: m.Migrator.Dialector.DataTypeOf(field)}

	if field.NotNull {
		expr.SQL += " NOT NULL"
	}

	if field.HasDefaultValue && (field.DefaultValueInterface != nil || field.DefaultValue != "") {
		if field.DefaultValue != "(-)" {
			expr.SQL += " DEFAULT " + field.DefaultValue
		}
	}

	return expr
}

// Open 创建一个新的 SQLite3 dialector
// dsn: 数据库连接字符串，支持相对路径和绝对路径
// 如果提供了 workingDir 参数（通过查询字符串），会使用 sqlite3-driver 的路径处理功能
func Open(dsn string) gorm.Dialector {
	return Dialector{DSN: dsn}
}
