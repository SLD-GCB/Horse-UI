package api

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/sysinfo"
)

// ensurePanelCert 首次启动（证书表为空）时自动生成一张自签证书，
// 并作为面板 HTTPS 的默认证书，实现「装好即可用 HTTPS」。
// 已有证书时不干预，避免覆盖用户自己的 ACME 证书。
func (a *App) ensurePanelCert() error {
	var n int64
	if err := a.db.Model(&model.Certificate{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	names := localCertNames()
	cfg, err := json.Marshal(map[string]any{"names": names, "days": 3650})
	if err != nil {
		return err
	}
	cert := model.Certificate{
		Kind:        model.CertKindSelfSigned,
		Domain:      "horseui-panel",
		Credentials: model.JSONText(cfg),
		AutoRenew:   true,
	}
	if err := a.db.Create(&cert).Error; err != nil {
		return err
	}
	if err := a.generateSelfSigned(&cert); err != nil {
		a.db.Delete(&model.Certificate{}, cert.ID)
		return err
	}
	if err := a.settings.SetDefault(model.SettingPanelTLSCertID, strconv.Itoa(int(cert.ID))); err != nil {
		return err
	}
	fmt.Printf("[horseui] 已自动生成面板自签证书（SAN: %s）\n", strings.Join(names, ", "))
	fmt.Printf("[horseui] 浏览器首次访问会提示证书不受信任，属自签证书的正常现象\n")
	return nil
}

// panelCertInfo 返回面板当前 HTTPS 证书的 SHA-256 指纹，供 HorseBox 双端做固定校验。
//
// 客户端设置里填的就是这个指纹：两端（Windows / Android）用它判断自签证书是否被替换。
func (a *App) panelCertInfo(c *gin.Context) {
	enabled := a.settings.Get(model.SettingPanelTLSEnable) == "1"
	out := gin.H{
		"enabled": enabled,
		"loaded":  a.panelTLS.has(),
		"cert_id": a.panelTLS.currentID(),
	}
	if enabled && c.Request.TLS == nil {
		out["warning"] = "当前请求没有经过面板自身的 HTTPS（可能由反向代理终止 TLS），这里显示的是面板内部证书，客户端实际看到的证书以代理配置为准。"
	}

	cert := a.panelTLS.current()
	if cert == nil || len(cert.Certificate) == 0 {
		ok(c, out)
		return
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		serverError(c, err)
		return
	}

	sum := sha256.Sum256(leaf.Raw)
	san := append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		san = append(san, ip.String())
	}
	out["sha256"] = colonHex(sum[:])
	out["subject"] = leaf.Subject.String()
	out["issuer"] = leaf.Issuer.String()
	out["san"] = san
	out["self_signed"] = string(leaf.RawIssuer) == string(leaf.RawSubject)
	out["not_before"] = leaf.NotBefore.Format(time.RFC3339)
	out["not_after"] = leaf.NotAfter.Format(time.RFC3339)
	ok(c, out)
}

// colonHex 把摘要格式化为 AA:BB:CC 形式，方便直接对照浏览器证书详情。
func colonHex(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// localCertNames 收集本机可用的证书 SAN：本机各网卡 IP + localhost + 主机名。
func localCertNames() []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add("localhost")
	add("127.0.0.1")
	for _, ifi := range sysinfo.Interfaces() {
		for _, addr := range ifi.Addresses {
			add(addr)
		}
	}
	if host, err := os.Hostname(); err == nil {
		add(host)
	}
	if len(out) == 0 {
		out = append(out, "localhost", "127.0.0.1")
	}
	return out
}
