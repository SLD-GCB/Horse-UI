// Package totp 实现 RFC 6238 的 6 位动态口令，不依赖第三方库。
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// Period 时间步长（秒）。
	Period = 30
	// Digits 动态码位数。
	Digits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret 生成 160 位随机密钥，Base32 编码。
func GenerateSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

func decodeSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	s = strings.TrimRight(s, "=")
	if s == "" {
		return nil, fmt.Errorf("totp: empty secret")
	}
	return b32.DecodeString(s)
}

// Hotp 计算指定时间步的动态码。
func Hotp(secret string, step int64) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, binaryCode%mod), nil
}

// Step 返回时间对应的时间步。
func Step(t time.Time) int64 { return t.Unix() / Period }

// Code 返回当前时间的动态码。
func Code(secret string, t time.Time) (string, error) {
	return Hotp(secret, Step(t))
}

// Validate 校验动态码，允许前后 skew 个时间步。
// 返回命中的时间步，调用方可用它做防重放。
func Validate(secret, code string, t time.Time, skew int) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return 0, false
	}
	step := Step(t)
	for offset := -skew; offset <= skew; offset++ {
		candidate, err := Hotp(secret, step+int64(offset))
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return step + int64(offset), true
		}
	}
	return 0, false
}

// URI 生成 otpauth:// 配置地址。
func URI(secret, issuer, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprintf("%d", Digits))
	query.Set("period", fmt.Sprintf("%d", Period))
	return "otpauth://totp/" + label + "?" + query.Encode()
}
