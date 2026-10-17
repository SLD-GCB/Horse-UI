package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const (
	passwordLower  = "abcdefghijkmnpqrstuvwxyz"
	passwordUpper  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	passwordDigits = "23456789"
	passwordSymbol = "!@#$%^&*-_=+"
)

// RandomPassword 生成 n 位随机密码（n<=0 时取 16），保证含大小写字母、数字与符号。
func RandomPassword(n int) string {
	if n < 8 {
		n = 16
	}
	all := passwordLower + passwordUpper + passwordDigits + passwordSymbol
	out := make([]byte, 0, n)
	// 先各取一位保证复杂度。
	for _, set := range []string{passwordLower, passwordUpper, passwordDigits, passwordSymbol} {
		c, err := randomChar(set)
		if err != nil {
			panic(err)
		}
		out = append(out, c)
	}
	for len(out) < n {
		c, err := randomChar(all)
		if err != nil {
			panic(err)
		}
		out = append(out, c)
	}
	// 洗牌，避免前四位固定为各类别。
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			panic(err)
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out)
}

func randomChar(set string) (byte, error) {
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
	if err != nil {
		return 0, err
	}
	return set[idx.Int64()], nil
}

// ValidatePassword 校验密码强度：至少 8 位，且同时包含字母、数字和符号。
func ValidatePassword(s string) error {
	if len([]rune(s)) < 8 {
		return fmt.Errorf("密码至少 8 位")
	}
	var hasLetter, hasDigit, hasSymbol bool
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	if !hasLetter || !hasDigit || !hasSymbol {
		return fmt.Errorf("密码需同时包含字母、数字和符号")
	}
	return nil
}

// WeakPasswords 已知弱默认口令列表（用于启动时检测）。
func WeakPasswords() []string {
	return []string{"admin", "123456", "password", "12345678", "admin123", "root"}
}

// IsWeakPassword 判断是否为明显弱口令（用于启动时提示）。
func IsWeakPassword(s string) bool {
	if len(s) < 8 {
		return true
	}
	lower := strings.ToLower(s)
	for _, w := range WeakPasswords() {
		if lower == w {
			return true
		}
	}
	return false
}
