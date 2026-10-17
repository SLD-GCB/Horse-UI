package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	certmgr "horseui/core/cert"
)

func (a *App) certDir() string { return filepath.Join(a.opts.DataDir, "certs") }

func (a *App) certManager() *certmgr.Manager {
	return certmgr.NewManager(a.certDir(), a.settings.Get(model.SettingACMEEmail))
}

func (a *App) listCerts(c *gin.Context) {
	var list []model.Certificate
	if err := a.db.Order("id").Find(&list).Error; err != nil {
		serverError(c, err)
		return
	}
	out := make([]model.Certificate, len(list))
	for i, ct := range list {
		out[i] = sanitizeCert(ct)
	}
	ok(c, out)
}

// sanitizeCert 脱敏证书凭据，避免 ACME 的 DNS API Token 明文回传前端。
func sanitizeCert(ct model.Certificate) model.Certificate {
	if ct.Kind == model.CertKindACME && ct.Credentials != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(ct.Credentials), &m); err == nil {
			for k, v := range m {
				if s, ok := v.(string); ok && s != "" {
					m[k] = maskSecret(s)
				}
			}
			if b, err := json.Marshal(m); err == nil {
				ct.Credentials = model.JSONText(b)
			}
		}
	}
	return ct
}

// maskSecret 保留末 4 位便于辨识，其余打码。
func maskSecret(s string) string {
	if len(s) <= 4 {
		return "******"
	}
	return "******" + s[len(s)-4:]
}

// acmeProviders 返回支持的 ACME 签发方式（自签由前端单独提供）。
func (a *App) certProviders(c *gin.Context) {
	ok(c, certmgr.Providers())
}

type certReq struct {
	Kind        string            `json:"kind"` // acme（默认）/ selfsigned
	Domain      string            `json:"domain"`
	Provider    string            `json:"provider"`
	Credentials map[string]string `json:"credentials"`
	Names       []string          `json:"names"` // 自签 SAN（域名或 IP）
	Days        int               `json:"days"`  // 自签有效期
	AutoRenew   bool              `json:"auto_renew"`
}

func (a *App) createCert(c *gin.Context) {
	var req certReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "参数错误")
		return
	}

	kind := req.Kind
	if kind == "" {
		kind = model.CertKindACME
	}

	if kind == model.CertKindSelfSigned {
		if len(cleanNames(req.Names)) == 0 {
			badRequest(c, "请至少填写一个域名或 IP 作为证书 SAN")
			return
		}
		a.createSelfSigned(c, req)
		return
	}

	// ---- ACME ----
	if strings.TrimSpace(req.Domain) == "" {
		badRequest(c, "请填写域名")
		return
	}
	if a.settings.Get(model.SettingACMEEmail) == "" {
		badRequest(c, "请先在「面板设置」填写 ACME 账户邮箱")
		return
	}

	creds, err := json.Marshal(req.Credentials)
	if err != nil {
		badRequest(c, "凭据格式错误")
		return
	}
	cert := model.Certificate{
		Kind:        model.CertKindACME,
		Domain:      strings.TrimSpace(req.Domain),
		Provider:    req.Provider,
		Credentials: model.JSONText(creds),
		AutoRenew:   req.AutoRenew,
	}
	if err := a.db.Create(&cert).Error; err != nil {
		serverError(c, err)
		return
	}
	if err := a.obtainCert(&cert); err != nil {
		a.db.Delete(&model.Certificate{}, cert.ID)
		fail(c, 500, err.Error())
		return
	}
	if a.core.Proc.Running() {
		_ = a.core.RestartCore()
	}
	ok(c, sanitizeCert(cert))
}

func (a *App) createSelfSigned(c *gin.Context, req certReq) {
	names := cleanNames(req.Names)

	label := strings.TrimSpace(req.Domain)
	if label == "" {
		label = names[0]
	}
	// 名称唯一，避免覆盖
	var n int64
	a.db.Model(&model.Certificate{}).Where("domain = ?", label).Count(&n)
	if n > 0 {
		label = label + "-" + randomPassword()[:6]
	}

	cfg, err := json.Marshal(map[string]any{"names": names, "days": req.Days})
	if err != nil {
		serverError(c, err)
		return
	}

	cert := model.Certificate{
		Kind:        model.CertKindSelfSigned,
		Domain:      label,
		Credentials: model.JSONText(cfg),
		AutoRenew:   req.AutoRenew,
	}
	if err := a.db.Create(&cert).Error; err != nil {
		serverError(c, err)
		return
	}
	if err := a.generateSelfSigned(&cert); err != nil {
		a.db.Delete(&model.Certificate{}, cert.ID)
		fail(c, 500, err.Error())
		return
	}
	if a.core.Proc.Running() {
		_ = a.core.RestartCore()
	}
	ok(c, sanitizeCert(cert))
}

func (a *App) renewCert(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var cert model.Certificate
	if err := a.db.First(&cert, id).Error; err != nil {
		fail(c, 404, "证书不存在")
		return
	}
	if err := a.obtainCert(&cert); err != nil {
		fail(c, 500, err.Error())
		return
	}
	if a.core.Proc.Running() {
		_ = a.core.RestartCore()
	}
	ok(c, sanitizeCert(cert))
}

func (a *App) deleteCert(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var used int64
	if err := a.db.Model(&model.Inbound{}).Where("cert_id = ?", id).Count(&used).Error; err != nil {
		serverError(c, err)
		return
	}
	if used > 0 {
		badRequest(c, "该证书正被入站引用，请先解除引用后再删除")
		return
	}
	if err := a.db.Delete(&model.Certificate{}, id).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"deleted": id})
}

// obtainCert 申请或续签证书（自签为重新生成），并把结果写回数据库。
func (a *App) obtainCert(cert *model.Certificate) error {
	if cert.Kind == model.CertKindSelfSigned {
		return a.generateSelfSigned(cert)
	}

	creds := map[string]string{}
	if cert.Credentials != "" {
		_ = json.Unmarshal([]byte(cert.Credentials), &creds)
	}
	res, err := a.certManager().Obtain(cert.Domain, cert.Provider, creds)
	if err != nil {
		return err
	}
	cert.CertPath = res.CertPath
	cert.KeyPath = res.KeyPath
	cert.NotBefore = &res.NotBefore
	cert.NotAfter = &res.NotAfter
	if err := a.db.Save(cert).Error; err != nil {
		return err
	}
	// 若该证书正用于面板 HTTPS，则热加载新证书。
	a.reloadPanelCertIf(cert.ID)
	return nil
}

// generateSelfSigned 生成自签证书并落盘。
func (a *App) generateSelfSigned(cert *model.Certificate) error {
	var cfg struct {
		Names []string `json:"names"`
		Days  int      `json:"days"`
	}
	if cert.Credentials != "" {
		_ = json.Unmarshal([]byte(cert.Credentials), &cfg)
	}
	if len(cleanNames(cfg.Names)) == 0 {
		cfg.Names = []string{cert.Domain}
	}

	res, err := certmgr.GenerateSelfSigned(certmgr.SelfSignedOptions{Names: cfg.Names, Days: cfg.Days})
	if err != nil {
		return err
	}

	dir := filepath.Join(a.certDir(), "selfsigned-"+sanitizeName(cert.Domain))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, res.CertPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, res.KeyPEM, 0o600); err != nil {
		return err
	}

	cert.CertPath = certPath
	cert.KeyPath = keyPath
	cert.NotBefore = &res.NotBefore
	cert.NotAfter = &res.NotAfter
	if err := a.db.Save(cert).Error; err != nil {
		return err
	}
	a.reloadPanelCertIf(cert.ID)
	return nil
}

func cleanNames(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		for _, p := range strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || r == '\n' || r == ' ' || r == '\t'
		}) {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "cert"
	}
	return b.String()
}
