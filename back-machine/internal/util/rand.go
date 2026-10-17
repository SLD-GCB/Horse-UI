package util

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"github.com/google/uuid"
)

// NewUUID 生成随机 UUID v4。
func NewUUID() string { return uuid.NewString() }

// RandomHex 生成 n 字节随机数的十六进制字符串（长度 2n）。
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// RandomBase64 生成 n 字节随机数的标准 base64 字符串。
func RandomBase64(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// RandomToken 生成订阅令牌等随机字符串。
func RandomToken() string { return RandomHex(16) }

// alnumCharset 大小写字母 + 数字，剔除 l/I/1/0/O 等易混淆字符，且不含 URL 特殊符号。
const alnumCharset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomAlnum 生成 n 位随机字母数字串（n<=0 时取 16）。
// 仅含大小写字母与数字，可直接用于客户端密码、订阅链接等需要 URL 安全的场景。
func RandomAlnum(n int) string {
	if n <= 0 {
		n = 16
	}
	b := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		c, err := randomChar(alnumCharset)
		if err != nil {
			panic(err)
		}
		b = append(b, c)
	}
	return string(b)
}
