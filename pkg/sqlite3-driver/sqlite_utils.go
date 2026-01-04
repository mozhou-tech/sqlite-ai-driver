package sqlite3_driver

import (
	"fmt"

	"github.com/google/uuid"
)

// uuidToBytes 将 UUID 转换为 16 字节二进制
func uuidToBytes(u uuid.UUID) []byte {
	return u[:]
}

// bytesToUUID 将 16 字节二进制转换为 UUID
func bytesToUUID(b []byte) (uuid.UUID, error) {
	if len(b) != 16 {
		return uuid.Nil, fmt.Errorf("invalid UUID length: expected 16 bytes, got %d", len(b))
	}
	var u uuid.UUID
	copy(u[:], b)
	return u, nil
}

// uuidStringToBytes 将 UUID 字符串转换为 16 字节二进制
// 用于在 CRUD 操作时将 UUID 字符串转换为 BLOB(16) 格式存储
func uuidStringToBytes(idStr string) ([]byte, error) {
	u, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID string: %w", err)
	}
	return uuidToBytes(u), nil
}

// bytesToUUIDString 将 16 字节二进制转换为 UUID 字符串
// 用于在 CRUD 操作时将 BLOB(16) 格式的 UUID 转换回字符串
func bytesToUUIDString(b []byte) (string, error) {
	u, err := bytesToUUID(b)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// UUIDStringToBytes 将 UUID 字符串转换为 16 字节二进制（导出版本）
// 用于在 CRUD 操作时将 UUID 字符串转换为 BLOB(16) 格式存储
func UUIDStringToBytes(idStr string) ([]byte, error) {
	return uuidStringToBytes(idStr)
}

// BytesToUUIDString 将 16 字节二进制转换为 UUID 字符串（导出版本）
// 用于在 CRUD 操作时将 BLOB(16) 格式的 UUID 转换回字符串
func BytesToUUIDString(b []byte) (string, error) {
	return bytesToUUIDString(b)
}
