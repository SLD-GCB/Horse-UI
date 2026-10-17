package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
)

const (
	auditBatchSize = 50
	auditFlushTick = time.Second
	auditMaxRows   = 10000
)

// startAudit 启动审计日志写入协程。
func (a *App) startAudit() {
	go a.auditLoop()
}

// auditLoop 批量把审计记录落库，并定期裁剪历史。
func (a *App) auditLoop() {
	flushTicker := time.NewTicker(auditFlushTick)
	trimTicker := time.NewTicker(time.Hour)
	defer flushTicker.Stop()
	defer trimTicker.Stop()

	buf := make([]model.AuditLog, 0, auditBatchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := buf
		buf = make([]model.AuditLog, 0, auditBatchSize)
		_ = a.db.Create(&batch).Error
	}
	for {
		select {
		case e := <-a.auditCh:
			buf = append(buf, e)
			if len(buf) >= auditBatchSize {
				flush()
			}
		case <-flushTicker.C:
			flush()
		case <-trimTicker.C:
			a.trimAudit()
		}
	}
}

// trimAudit 只保留最近 auditMaxRows 条记录。
func (a *App) trimAudit() {
	var count int64
	if err := a.db.Model(&model.AuditLog{}).Count(&count).Error; err != nil || count <= auditMaxRows {
		return
	}
	var threshold model.AuditLog
	offset := int(count - auditMaxRows)
	if err := a.db.Order("id").Offset(offset).Limit(1).Find(&threshold).Error; err != nil || threshold.ID == 0 {
		return
	}
	a.db.Where("id < ?", threshold.ID).Delete(&model.AuditLog{})
}

// recordAudit 非阻塞投递一条审计记录；队列满时丢弃，避免拖慢请求。
func (a *App) recordAudit(e model.AuditLog) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	select {
	case a.auditCh <- e:
	default:
	}
}

// auditMiddleware 记录非 GET 的 API 操作。
func (a *App) auditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodOptions {
			return
		}
		a.recordAudit(model.AuditLog{
			Username:  c.GetString("username"),
			IP:        c.ClientIP(),
			Method:    c.Request.Method,
			Path:      c.FullPath(),
			Action:    shortAction(c.HandlerName()),
			Status:    c.Writer.Status(),
			Detail:    auditDetail(c),
			CreatedAt: time.Now(),
		})
	}
}

func auditDetail(c *gin.Context) string {
	if id := c.Param("id"); id != "" {
		return "id=" + id
	}
	return ""
}

func shortAction(handler string) string {
	name := handler
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	// Go 对方法值生成的包装函数名带 "-fm" 后缀，去掉更易读。
	return strings.TrimSuffix(name, "-fm")
}

// listAudit 分页查询审计日志。
func (a *App) listAudit(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}

	q := a.db.Model(&model.AuditLog{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("username LIKE ? OR path LIKE ? OR action LIKE ?", like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		serverError(c, err)
		return
	}

	var rows []model.AuditLog
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"total": total, "items": rows, "page": page, "size": size})
}
