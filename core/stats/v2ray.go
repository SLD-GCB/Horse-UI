package stats

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// servicePath sing-box V2Ray 统计服务的 gRPC 方法前缀。
const servicePath = "/v2ray.core.app.stats.command.StatsService/"

// Client sing-box V2Ray 统计 API 客户端。
type Client struct {
	mu   sync.Mutex
	addr string
	conn *grpc.ClientConn
}

// NewClient 创建统计客户端。
func NewClient(addr string) *Client { return &Client{addr: addr} }

// Addr 返回监听地址。
func (c *Client) Addr() string { return c.addr }

// Close 关闭底层连接，下次调用会自动重连。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, req []byte) ([]byte, error) {
	c.mu.Lock()
	if c.conn == nil {
		conn, err := grpc.NewClient(c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.conn = conn
	}
	conn := c.conn
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var resp []byte
	if err := conn.Invoke(ctx, servicePath+method, req, &resp, grpc.ForceCodec(rawCodec{})); err != nil {
		// 连接可能已失效，丢弃以便下次重建。
		_ = c.Close()
		return nil, err
	}
	return resp, nil
}

// Query 按模式查询统计项并返回 名称->值 映射。
func (c *Client) Query(ctx context.Context, pattern string, reset bool) (map[string]int64, error) {
	resp, err := c.call(ctx, "QueryStats", marshalQueryStatsRequest(pattern, reset))
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64)
	for _, s := range unmarshalQueryStatsResponse(resp) {
		if s.Name != "" {
			out[s.Name] = s.Value
		}
	}
	return out, nil
}

// Get 查询单项统计。
func (c *Client) Get(ctx context.Context, name string, reset bool) (int64, error) {
	resp, err := c.call(ctx, "GetStats", marshalGetStatsRequest(name, reset))
	if err != nil {
		return 0, err
	}
	s, ok := unmarshalGetStatsResponse(resp)
	if !ok {
		return 0, fmt.Errorf("统计项 %s 不存在", name)
	}
	return s.Value, nil
}

// UserTraffic 单个用户的累计流量。
type UserTraffic struct {
	Up   int64
	Down int64
}

// QueryUserTraffic 查询所有被跟踪用户的累计上/下行流量。
// 统计项命名为 user>>>{用户名}>>>traffic>>>{uplink|downlink}。
func (c *Client) QueryUserTraffic(ctx context.Context) (map[string]UserTraffic, error) {
	return c.queryTraffic(ctx, "user")
}

// QueryInboundTraffic 查询所有被跟踪入站的累计上/下行流量。
func (c *Client) QueryInboundTraffic(ctx context.Context) (map[string]UserTraffic, error) {
	return c.queryTraffic(ctx, "inbound")
}

func (c *Client) queryTraffic(ctx context.Context, kind string) (map[string]UserTraffic, error) {
	raw, err := c.Query(ctx, kind+">>>", false)
	if err != nil {
		return nil, err
	}
	out := make(map[string]UserTraffic)
	for name, v := range raw {
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[0] != kind || parts[2] != "traffic" {
			continue
		}
		t := out[parts[1]]
		switch parts[3] {
		case "uplink":
			t.Up = v
		case "downlink":
			t.Down = v
		default:
			continue
		}
		out[parts[1]] = t
	}
	return out, nil
}
