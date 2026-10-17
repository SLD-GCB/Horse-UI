package svc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"horseui/back-machine/internal/model"
)

// adblockRoute 生成广告过滤的 rule_set 定义与拦截规则；未启用或内容为空时返回空。
// 采用「本地规则集」以避免远程下载失败导致内核无法启动；远程规则集为可选项。
func (c *Core) adblockRoute() (rules []any, ruleSets []any) {
	if c.Settings.Get(model.SettingAdblockEnable) != "1" {
		return nil, nil
	}

	var tags []string

	if objs := c.adblockRuleObjects(); len(objs) > 0 {
		if path, err := c.writeAdblockRuleset(objs); err == nil {
			ruleSets = append(ruleSets, map[string]any{
				"type":   "local",
				"tag":    "adblock",
				"format": "source",
				"path":   path,
			})
			tags = append(tags, "adblock")
		}
	}

	// 可选的远程规则集（.srs）；地址不可达时可能导致内核启动失败，UI 已提示。
	if u := strings.TrimSpace(c.Settings.Get(model.SettingAdblockURL)); u != "" {
		ruleSets = append(ruleSets, map[string]any{
			"type":            "remote",
			"tag":             "adblock-remote",
			"format":          "binary",
			"url":             u,
			"download_detour": "direct",
			"update_interval": "1d",
		})
		tags = append(tags, "adblock-remote")
	}

	if len(tags) == 0 {
		return nil, nil
	}
	rules = append(rules, map[string]any{"rule_set": tags, "action": "reject"})
	return rules, ruleSets
}

// adblockRuleObjects 把四类设置转成规则集内的规则对象（彼此是"或"关系）。
func (c *Core) adblockRuleObjects() []any {
	var out []any
	if d := parseDomainLines(c.Settings.Get(model.SettingAdblockDomains)); len(d) > 0 {
		out = append(out, map[string]any{"domain_suffix": d})
	}
	if k := parsePlainList(c.Settings.Get(model.SettingAdblockKeywords)); len(k) > 0 {
		out = append(out, map[string]any{"domain_keyword": k})
	}
	if r := parseRegexLines(c.Settings.Get(model.SettingAdblockRegex)); len(r) > 0 {
		out = append(out, map[string]any{"domain_regex": r})
	}
	if ips := parsePlainList(c.Settings.Get(model.SettingAdblockIPs)); len(ips) > 0 {
		out = append(out, map[string]any{"ip_cidr": ips})
	}
	return out
}

// writeAdblockRuleset 生成本地规则集文件（sing-box source 格式）。
func (c *Core) writeAdblockRuleset(rules []any) (string, error) {
	dir := filepath.Join(c.DataDir, "rulesets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "adblock.json")
	content := map[string]any{"version": 1, "rules": rules}
	b, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// parseDomainLines 解析域名后缀（小写、去首尾点；支持换行/逗号/空格分隔，# 注释）。
func parseDomainLines(raw string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range splitLines(raw) {
		d := strings.ToLower(strings.Trim(item, "."))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// parsePlainList 解析关键词 / IP 列表（保留原样，仅去空白；支持换行/逗号/空格分隔）。
func parsePlainList(raw string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range splitLines(raw) {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// parseRegexLines 解析正则列表：**逐行**处理（正则内部可能含逗号/空格，不能按分隔符切）。
func parseRegexLines(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitLines 按行并进一步按逗号/空白拆分，跳过空行与 # 注释。
func splitLines(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, part := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		}) {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// AdblockPresetDomains 返回内置的常用广告/追踪域名。
func AdblockPresetDomains() []string {
	return []string{
		// Google 广告
		"doubleclick.net", "googleadservices.com", "googlesyndication.com",
		"google-analytics.com", "googletagmanager.com", "googletagservices.com",
		"adservice.google.com", "pagead2.googlesyndication.com", "2mdn.net",
		// 社交/追踪
		"connect.facebook.net", "analytics.twitter.com", "ads.twitter.com",
		"ads.linkedin.com", "px.ads.linkedin.com", "bat.bing.com",
		// 广告交易/投放
		"adsrvr.org", "adnxs.com", "rubiconproject.com", "pubmatic.com",
		"openx.net", "criteo.com", "criteo.net", "taboola.com", "outbrain.com",
		"scorecardresearch.com", "quantserve.com", "moatads.com", "adform.net",
		"smartadserver.com", "teads.tv", "sharethrough.com", "zedo.com",
		"casalemedia.com", "contextweb.com", "indexexchange.com", "sovrn.com",
		"gumgum.com", "media.net", "bidswitch.net", "adcolony.com",
		"applovin.com", "unityads.unity3d.com", "vungle.com", "chartboost.com",
		"inmobi.com", "adsafeprotected.com", "amazon-adsystem.com",
		// 数据管理/画像
		"bluekai.com", "exelator.com", "demdex.net", "omtrdc.net",
		"everesttech.net", "adobedtm.com", "mathtag.com", "turn.com",
		"tribalfusion.com", "adzerk.net", "simplify360.com",
		// 国内常见
		"umeng.com", "umengcloud.com", "cnzz.com", "tongji.baidu.com",
		"hm.baidu.com", "pos.baidu.com", "cpro.baidu.com", "union.baidu.com",
		"mobads.qq.com", "gdt.qq.com", "e.qq.com", "ta.qq.com",
		"pingfore.qq.com", "miaozhen.com", "admaster.com.cn", "adsame.com",
		"allyes.com", "mediav.com", "adchina.com", "madhouse.com.cn",
		"irs01.com", "51.la", "51yes.com", "lianmeng.360.cn",
	}
}

// AdblockPresetKeywords 返回内置的广告域名关键词（保守，避免误伤正常域名）。
func AdblockPresetKeywords() []string {
	return []string{
		"adserver", "adservice", "adclick", "adtrack", "adframe", "adbanner",
		"adzone", "adhost", "adcdn", "adsrv", "adnxs", "adform", "adtech",
		"advert", "doubleclick", "googlesyndication", "adcolony", "applovin",
		"popads", "popcash", "propellerads",
	}
}
