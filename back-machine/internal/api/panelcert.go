package api

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

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
