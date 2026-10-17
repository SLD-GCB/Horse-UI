package config

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// RealityKeypair 生成 Reality 所需的 X25519 密钥对，
// 使用与 sing-box 一致的 base64 raw url 编码（43 字符）。
func RealityKeypair() (privateKey, publicKey string, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

// RealityPublicFromPrivate 由私钥推导公钥。
func RealityPublicFromPrivate(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("私钥解码失败: %w", err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

// ShortID 生成 Reality short id（8 字节 → 16 位十六进制）。
func ShortID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// SSPassword 按 Shadowsocks 2022 方法生成对应长度的 base64 密码。
// 非 2022 方法返回空串与 nil。
func SSPassword(method string) (string, error) {
	var n int
	switch method {
	case "2022-blake3-aes-128-gcm":
		n = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		n = 32
	default:
		return "", nil
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// PresetSSMethods Shadowsocks 可选方法。
func PresetSSMethods() []string {
	return []string{
		"2022-blake3-aes-128-gcm",
		"2022-blake3-aes-256-gcm",
		"2022-blake3-chacha20-poly1305",
		"aes-128-gcm",
		"aes-256-gcm",
		"chacha20-ietf-poly1305",
		"xchacha20-ietf-poly1305",
	}
}
