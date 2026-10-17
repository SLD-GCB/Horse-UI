package api

import (
	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/util"
	"horseui/core/config"
)

// randomPassword 生成 16 位随机客户端密码（大小写字母 + 数字，URL 安全，不含符号）。
func randomPassword() string { return util.RandomAlnum(16) }

// fillClientSecrets 依据入站协议补全客户端密钥与令牌。
func fillClientSecrets(ib model.Inbound, cl *model.Client) error {
	switch ib.Type {
	case model.ProtoVLESS, model.ProtoVMess:
		if cl.UUID == "" {
			cl.UUID = util.NewUUID()
		}
	case model.ProtoTrojan, model.ProtoHysteria2, model.ProtoHTTP:
		if cl.Password == "" {
			cl.Password = randomPassword()
		}
	case model.ProtoShadowsocks:
		if cl.Password == "" {
			pwd, err := config.SSPassword(ib.Method)
			if err != nil {
				return err
			}
			if pwd == "" {
				pwd = randomPassword()
			}
			cl.Password = pwd
		}
	case model.ProtoTUIC:
		if cl.UUID == "" {
			cl.UUID = util.NewUUID()
		}
		if cl.Password == "" {
			cl.Password = randomPassword()
		}
	}

	if cl.SubToken == "" {
		cl.SubToken = util.RandomToken()
	}
	if cl.Name == "" {
		cl.Name = "user-" + util.RandomHex(3)
	}
	return nil
}
