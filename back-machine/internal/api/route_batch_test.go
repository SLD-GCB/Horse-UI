package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestBatchRouteConflict 确认 /clients|inbounds|outbounds/batch 与 :id 路由能共存。
func TestBatchRouteConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	g.POST("/clients", func(c *gin.Context) { c.String(200, "create") })
	g.POST("/clients/batch", func(c *gin.Context) { c.String(200, "batch") })
	g.POST("/clients/:id/reset-traffic", func(c *gin.Context) { c.String(200, "reset") })

	g.POST("/inbounds", func(c *gin.Context) { c.String(200, "create") })
	g.POST("/inbounds/batch", func(c *gin.Context) { c.String(200, "batch") })
	g.POST("/inbounds/:id/test", func(c *gin.Context) { c.String(200, "test") })

	g.POST("/outbounds", func(c *gin.Context) { c.String(200, "create") })
	g.POST("/outbounds/batch", func(c *gin.Context) { c.String(200, "batch") })
	g.POST("/outbounds/test", func(c *gin.Context) { c.String(200, "test") })

	cases := []struct {
		path string
		want string
	}{
		{"/api/clients/batch", "batch"},
		{"/api/clients/12/reset-traffic", "reset"},
		{"/api/inbounds/batch", "batch"},
		{"/api/inbounds/3/test", "test"},
		{"/api/outbounds/batch", "batch"},
		{"/api/outbounds/test", "test"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", tc.path, nil))
		if w.Body.String() != tc.want {
			t.Fatalf("%s 匹配错误：%q", tc.path, w.Body.String())
		}
	}
}
