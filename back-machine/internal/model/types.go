package model

// JSONText 以原始 JSON 文本形式读写数据库字段。
// 存库时为普通字符串，对外 JSON 序列化时直接输出原始 JSON。
type JSONText string

// MarshalJSON 输出原始 JSON；空值输出 null。
func (j JSONText) MarshalJSON() ([]byte, error) {
	if j == "" {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

// UnmarshalJSON 接收原始 JSON 文本。
func (j *JSONText) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*j = ""
		return nil
	}
	*j = JSONText(b)
	return nil
}

// String 返回底层文本。
func (j JSONText) String() string { return string(j) }
