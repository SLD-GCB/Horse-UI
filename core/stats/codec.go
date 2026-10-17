package stats

import "fmt"

// rawCodec 直接透传手工编码的 protobuf 字节。
// 这样无需引入 protobuf 运行时与 .pb.go 生成代码。
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("stats: 期望 []byte，实际 %T", v)
	}
	return b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("stats: 期望 *[]byte，实际 %T", v)
	}
	*p = append([]byte(nil), data...)
	return nil
}

func (rawCodec) Name() string { return "proto" }

// ---------------- protobuf 写入 ----------------

func pbVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func pbTag(b []byte, field, wire int) []byte {
	return pbVarint(b, uint64(field)<<3|uint64(wire))
}

func pbString(b []byte, field int, s string) []byte {
	if s == "" {
		return b
	}
	b = pbTag(b, field, 2)
	b = pbVarint(b, uint64(len(s)))
	return append(b, s...)
}

func pbBool(b []byte, field int, v bool) []byte {
	if !v {
		return b
	}
	b = pbTag(b, field, 0)
	return append(b, 1)
}

// ---------------- protobuf 读取 ----------------

type pbReader struct {
	b []byte
	i int
}

func (r *pbReader) done() bool { return r.i >= len(r.b) }

func (r *pbReader) varint() (uint64, bool) {
	var v uint64
	var shift uint
	for r.i < len(r.b) {
		c := r.b[r.i]
		r.i++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, true
		}
		shift += 7
		if shift > 63 {
			return 0, false
		}
	}
	return 0, false
}

func (r *pbReader) tag() (field, wire int, ok bool) {
	v, ok := r.varint()
	if !ok {
		return 0, 0, false
	}
	return int(v >> 3), int(v & 7), true
}

func (r *pbReader) lenBytes() ([]byte, bool) {
	n, ok := r.varint()
	if !ok || uint64(len(r.b)-r.i) < n {
		return nil, false
	}
	s := r.b[r.i : r.i+int(n)]
	r.i += int(n)
	return s, true
}

func (r *pbReader) skip(wire int) bool {
	switch wire {
	case 0:
		_, ok := r.varint()
		return ok
	case 1:
		if len(r.b)-r.i < 8 {
			return false
		}
		r.i += 8
		return true
	case 2:
		_, ok := r.lenBytes()
		return ok
	case 5:
		if len(r.b)-r.i < 4 {
			return false
		}
		r.i += 4
		return true
	default:
		return false
	}
}

// ---------------- v2ray stats 消息 ----------------

// Stat 单条统计项。
type Stat struct {
	Name  string
	Value int64
}

func marshalGetStatsRequest(name string, reset bool) []byte {
	var b []byte
	b = pbString(b, 1, name)
	b = pbBool(b, 2, reset)
	return b
}

func marshalQueryStatsRequest(pattern string, reset bool) []byte {
	var b []byte
	b = pbString(b, 1, pattern)
	b = pbBool(b, 2, reset)
	return b
}

func unmarshalStat(data []byte) Stat {
	var s Stat
	r := &pbReader{b: data}
	for !r.done() {
		f, w, ok := r.tag()
		if !ok {
			return s
		}
		switch {
		case f == 1 && w == 2:
			v, ok := r.lenBytes()
			if !ok {
				return s
			}
			s.Name = string(v)
		case f == 2 && w == 0:
			v, ok := r.varint()
			if !ok {
				return s
			}
			s.Value = int64(v)
		default:
			if !r.skip(w) {
				return s
			}
		}
	}
	return s
}

func unmarshalGetStatsResponse(data []byte) (Stat, bool) {
	r := &pbReader{b: data}
	for !r.done() {
		f, w, ok := r.tag()
		if !ok {
			return Stat{}, false
		}
		if f == 1 && w == 2 {
			v, ok := r.lenBytes()
			if !ok {
				return Stat{}, false
			}
			return unmarshalStat(v), true
		}
		if !r.skip(w) {
			return Stat{}, false
		}
	}
	return Stat{}, false
}

func unmarshalQueryStatsResponse(data []byte) []Stat {
	var out []Stat
	r := &pbReader{b: data}
	for !r.done() {
		f, w, ok := r.tag()
		if !ok {
			break
		}
		if f == 1 && w == 2 {
			v, ok := r.lenBytes()
			if !ok {
				break
			}
			out = append(out, unmarshalStat(v))
			continue
		}
		if !r.skip(w) {
			break
		}
	}
	return out
}

func marshalStat(s Stat) []byte {
	var b []byte
	b = pbString(b, 1, s.Name)
	if s.Value != 0 {
		b = pbTag(b, 2, 0)
		b = pbVarint(b, uint64(s.Value))
	}
	return b
}
