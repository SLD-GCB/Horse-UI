package api

import "errors"

// errString 构造简单错误。
func errString(s string) error { return errors.New(s) }

// errStr 返回错误文本，nil 返回空串。
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
