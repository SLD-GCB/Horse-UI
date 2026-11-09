package ipcare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"horseui/back-machine/internal/model"
)

// ErrBusy 已有检测在运行。
var ErrBusy = errors.New("ip care task is running")

const (
	defaultTarget   = "US"
	defaultDetect   = "manual"
	defaultInterval = 86400
	defaultWarm     = "manual"

	probeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var regionRe = regexp.MustCompile(`"(?:contentRegion|countryCode|INNERTUBE_CONTEXT_GL|GL)"\s*:\s*"([A-Za-z]{2})"`)

var warmKeywords = map[string][]string{
	"US": {"weather today", "local news", "coffee shops", "public library", "weekend events"},
	"HK": {"今日天氣", "香港新聞", "附近餐廳", "周末活動", "公共交通"},
	"JP": {"今日の天気", "近くのニュース", "カフェ", "週末 イベント", "電車 時刻表"},
	"SG": {"weather today", "local news", "hawker food", "weekend events", "public transport"},
	"TW": {"今日天氣", "台灣新聞", "附近餐廳", "週末活動", "大眾運輸"},
	"GB": {"weather today", "local news", "coffee shops", "weekend events", "train times"},
	"DE": {"Wetter heute", "lokale Nachrichten", "Cafés", "Veranstaltungen", "Zugverbindung"},
	"KR": {"오늘 날씨", "지역 뉴스", "카페", "주말 행사", "지하철 시간"},
	"CA": {"weather today", "local news", "coffee shops", "weekend events", "transit"},
	"AU": {"weather today", "local news", "coffee shops", "weekend events", "public transport"},
}

var regionLanguages = map[string]string{
	"US": "en-US",
	"HK": "zh-HK",
	"JP": "ja-JP",
	"SG": "en-SG",
	"TW": "zh-TW",
	"GB": "en-GB",
	"DE": "de-DE",
	"KR": "ko-KR",
	"CA": "en-CA",
	"AU": "en-AU",
}

var regionNewsLanguages = map[string]string{
	"US": "en",
	"HK": "zh-Hant",
	"JP": "ja",
	"SG": "en",
	"TW": "zh-Hant",
	"GB": "en",
	"DE": "de",
	"KR": "ko",
	"CA": "en",
	"AU": "en",
}

type geoPoint struct {
	lat float64
	lon float64
}

var regionPoints = map[string]geoPoint{
	"US": {37.7749, -122.4194},
	"HK": {22.3193, 114.1694},
	"JP": {35.6762, 139.6503},
	"SG": {1.3521, 103.8198},
	"TW": {25.0330, 121.5654},
	"GB": {51.5072, -0.1276},
	"DE": {52.5200, 13.4050},
	"KR": {37.5665, 126.9780},
	"CA": {43.6532, -79.3832},
	"AU": {-33.8688, 151.2093},
}

var warmAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:126.0) Gecko/20100101 Firefox/126.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
}

// Status 前端展示用的聚合状态。
type Status struct {
	IP           string               `json:"ip"`
	GoogleRegion string               `json:"google_region"`
	TargetRegion string               `json:"target_region"`
	IsCN         bool                 `json:"is_cn"`
	TrustScore   int                  `json:"trust_score"`
	LastCheck    int64                `json:"last_check"`
	LastWarm     int64                `json:"last_warm"`
	Busy         bool                 `json:"busy"`
	LastError    string               `json:"last_error"`
	Settings     model.IPCareSettings `json:"settings"`
	Last         *model.IPCareCheck   `json:"last,omitempty"`
}

// Service IP 养护检测服务。
type Service struct {
	db      *gorm.DB
	dataDir string
	client  *http.Client

	mu   sync.Mutex
	busy bool
}

// New 创建服务。
func New(db *gorm.DB, dataDir string) *Service {
	return &Service{
		db:      db,
		dataDir: dataDir,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Start 启动自动检测循环。
func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}

// GetSettings 返回设置，不存在时创建默认值。
func (s *Service) GetSettings() (model.IPCareSettings, error) {
	var out model.IPCareSettings
	err := s.db.Where("id = ?", 1).First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		out = model.IPCareSettings{
			ID:                    1,
			TargetRegion:          defaultTarget,
			DetectMode:            defaultDetect,
			DetectIntervalSeconds: defaultInterval,
			WarmMode:              defaultWarm,
			NotifyCN:              true,
			NotifyFail:            true,
			UpdatedAt:             time.Now().Unix(),
		}
		if err := s.db.Create(&out).Error; err != nil {
			return out, err
		}
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if out.TargetRegion == "" {
		out.TargetRegion = defaultTarget
	}
	if out.DetectMode == "" {
		out.DetectMode = defaultDetect
	}
	if out.DetectIntervalSeconds <= 0 {
		out.DetectIntervalSeconds = defaultInterval
	}
	if out.WarmMode == "" {
		out.WarmMode = defaultWarm
	}
	return out, nil
}

// SaveSettings 校验并保存设置。
func (s *Service) SaveSettings(in model.IPCareSettings) (model.IPCareSettings, error) {
	region := strings.ToUpper(strings.TrimSpace(in.TargetRegion))
	if len(region) != 2 || region[0] < 'A' || region[0] > 'Z' || region[1] < 'A' || region[1] > 'Z' {
		return model.IPCareSettings{}, fmt.Errorf("目标区域必须是两位国家代码")
	}
	mode := strings.ToLower(strings.TrimSpace(in.DetectMode))
	switch mode {
	case "manual", "20m", "hourly", "6h", "12h", "daily", "weekly":
	default:
		return model.IPCareSettings{}, fmt.Errorf("检测模式不合法")
	}
	switch mode {
	case "20m":
		in.DetectIntervalSeconds = 1200
	case "hourly":
		in.DetectIntervalSeconds = 3600
	case "6h":
		in.DetectIntervalSeconds = 21600
	case "12h":
		in.DetectIntervalSeconds = 43200
	case "daily":
		in.DetectIntervalSeconds = 86400
	case "weekly":
		in.DetectIntervalSeconds = 604800
	}
	if mode == "manual" && in.DetectIntervalSeconds <= 0 {
		in.DetectIntervalSeconds = defaultInterval
	}
	if in.DetectIntervalSeconds < 1200 {
		in.DetectIntervalSeconds = 1200
	}
	if in.DetectIntervalSeconds > 2592000 {
		in.DetectIntervalSeconds = 2592000
	}
	warm := strings.ToLower(strings.TrimSpace(in.WarmMode))
	if warm != "auto" {
		warm = "manual"
	}

	out := model.IPCareSettings{
		ID:                    1,
		TargetRegion:          region,
		DetectMode:            mode,
		DetectIntervalSeconds: in.DetectIntervalSeconds,
		WarmMode:              warm,
		NotifyCN:              in.NotifyCN,
		NotifyFail:            in.NotifyFail,
		UpdatedAt:             time.Now().Unix(),
	}
	if err := s.db.Save(&out).Error; err != nil {
		return out, err
	}
	return out, nil
}

// Status 返回当前状态和最近一次结果。
func (s *Service) Status() (Status, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return Status{}, err
	}
	var last model.IPCareCheck
	lastErr := s.db.Order("id DESC").First(&last).Error
	if lastErr != nil && !errors.Is(lastErr, gorm.ErrRecordNotFound) {
		return Status{}, lastErr
	}
	out := Status{
		TargetRegion: settings.TargetRegion,
		Settings:     settings,
		TrustScore:   -1,
	}
	if lastErr == nil {
		out.IP = last.IP
		out.GoogleRegion = last.GoogleRegion
		out.IsCN = last.IsCN
		out.TrustScore = last.TrustScore
		out.LastCheck = last.CreatedAt
		out.LastError = last.Error
		out.Last = &last
	}
	var lastWarm model.IPCareJob
	if err := s.db.Where("type = ? AND status = ?", "warm", "success").Order("id DESC").First(&lastWarm).Error; err == nil {
		out.LastWarm = lastWarm.FinishedAt
	}
	s.mu.Lock()
	out.Busy = s.busy
	s.mu.Unlock()
	return out, nil
}

// History 返回最近检测记录。
func (s *Service) History(limit int) ([]model.IPCareCheck, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []model.IPCareCheck
	err := s.db.Order("id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// WarmHistory 返回最近纠偏任务。
func (s *Service) WarmHistory(limit int) ([]model.IPCareJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []model.IPCareJob
	err := s.db.Where("type = ?", "warm").Order("id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// Warm 执行一次温和的 Google 访问，改变 IP 区域画像。
func (s *Service) Warm(ctx context.Context) (model.IPCareJob, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return model.IPCareJob{}, ErrBusy
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	settings, err := s.GetSettings()
	if err != nil {
		return model.IPCareJob{}, err
	}
	job := model.IPCareJob{
		Type:      "warm",
		Status:    "running",
		CreatedAt: time.Now().Unix(),
	}
	if err := s.db.Create(&job).Error; err != nil {
		return job, err
	}
	finish := func(status, msg string, requests int) (model.IPCareJob, error) {
		job.Status = status
		job.Error = msg
		job.Requests = requests
		job.FinishedAt = time.Now().Unix()
		_ = s.db.Save(&job).Error
		if status == "failed" {
			return job, errors.New(msg)
		}
		return job, nil
	}

	region := settings.TargetRegion
	keywords := warmKeywords[region]
	if len(keywords) == 0 {
		keywords = warmKeywords[defaultTarget]
	}
	var last model.IPCareCheck
	_ = s.db.Order("id DESC").First(&last).Error
	persona := personaFor(last.IP, region)
	jar, err := s.loadJar()
	if err != nil {
		return finish("failed", "读取会话失败: "+err.Error(), 0)
	}
	client := &http.Client{Timeout: 12 * time.Second, Jar: jar}
	success := 0
	var errs []string
	count := 4
	if settings.DetectIntervalSeconds <= 1200 {
		count = 1
	} else if settings.DetectIntervalSeconds <= 3600 {
		count = 2
	} else if settings.DetectIntervalSeconds <= 21600 {
		count = 3
	}
	start := 0
	if count < 4 {
		start = rand.Intn(4)
	}
	referer := ""
	for i := 0; i < count; i++ {
		keyword := keywords[rand.Intn(len(keywords))]
		target := warmURL(region, keyword, (start+i)%4)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		req.Header.Set("User-Agent", persona.ua)
		req.Header.Set("Accept-Language", persona.lang)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Sec-CH-UA-Mobile", "?0")
		req.Header.Set("Sec-CH-UA-Platform", persona.platform)
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				success++
				referer = target
			} else {
				errs = append(errs, fmt.Sprintf("%s: HTTP %d", target, resp.StatusCode))
			}
		}
		if i < count-1 {
			select {
			case <-ctx.Done():
				_ = s.saveJar(jar)
				return finish("failed", ctx.Err().Error(), success)
			case <-time.After(time.Duration(3000+rand.Intn(9000)) * time.Millisecond):
			}
		}
	}
	_ = s.saveJar(jar)
	if success == 0 {
		return finish("failed", strings.Join(errs, "; "), 0)
	}
	return finish("success", strings.Join(errs, "; "), success)
}

func warmURL(region, keyword string, index int) string {
	q := url.QueryEscape(keyword)
	gl := strings.ToLower(region)
	hl := regionLanguages[region]
	if hl == "" {
		hl = "en-US"
	}
	newsLang := regionNewsLanguages[region]
	if newsLang == "" {
		newsLang = "en"
	}
	switch index {
	case 0:
		return fmt.Sprintf("https://www.google.com/?hl=%s&gl=%s", hl, gl)
	case 1:
		return fmt.Sprintf("https://www.google.com/search?q=%s&hl=%s&gl=%s", q, hl, gl)
	case 2:
		return fmt.Sprintf("https://news.google.com/?hl=%s&gl=%s&ceid=%s:%s", hl, gl, strings.ToUpper(gl), newsLang)
	default:
		if point, ok := regionPoints[region]; ok {
			return fmt.Sprintf(
				"https://www.google.com/maps/search/%s/@%.4f,%.4f,15z",
				url.PathEscape(keyword), point.lat, point.lon,
			)
		}
		return "https://www.google.com/maps/search/" + url.PathEscape(keyword)
	}
}

func acceptLanguage(region string) string {
	lang := regionLanguages[region]
	if lang == "" {
		lang = "en-US"
	}
	base := strings.Split(lang, "-")[0]
	return fmt.Sprintf("%s,%s;q=0.9,en;q=0.8", lang, base)
}

type persona struct {
	ua       string
	lang     string
	platform string
}

func personaFor(ip, region string) persona {
	h := fnv.New32a()
	_, _ = h.Write([]byte(ip + "|" + region))
	ua := warmAgents[int(h.Sum32())%len(warmAgents)]
	platform := `"Windows"`
	if strings.Contains(ua, "Macintosh") {
		platform = `"macOS"`
	}
	return persona{ua: ua, lang: acceptLanguage(region), platform: platform}
}

func (s *Service) cookiePath() string {
	return filepath.Join(s.dataDir, "ipcare_cookies.json")
}

type storedCookieJar struct {
	URL     string         `json:"url"`
	Cookies []*http.Cookie `json:"cookies"`
}

func (s *Service) loadJar() (*cookiejar.Jar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.cookiePath())
	if err != nil {
		if os.IsNotExist(err) {
			return jar, nil
		}
		return nil, err
	}
	var entries []storedCookieJar
	if err := json.Unmarshal(raw, &entries); err != nil {
		return jar, nil
	}
	for _, entry := range entries {
		u, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		jar.SetCookies(u, entry.Cookies)
	}
	return jar, nil
}

func (s *Service) saveJar(jar *cookiejar.Jar) error {
	urls := []string{
		"https://www.google.com/",
		"https://news.google.com/",
		"https://www.youtube.com/",
	}
	entries := make([]storedCookieJar, 0, len(urls))
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if cookies := jar.Cookies(u); len(cookies) > 0 {
			entries = append(entries, storedCookieJar{URL: raw, Cookies: cookies})
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.cookiePath(), data, 0o600)
}

// Check 执行一次检测并落库。
func (s *Service) Check(ctx context.Context) (model.IPCareCheck, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return model.IPCareCheck{}, ErrBusy
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	settings, err := s.GetSettings()
	if err != nil {
		return model.IPCareCheck{}, err
	}
	start := time.Now()
	out := model.IPCareCheck{
		TargetRegion: settings.TargetRegion,
		TrustScore:   -1,
		CreatedAt:    time.Now().Unix(),
	}

	ip, err := s.publicIP(ctx)
	if err != nil {
		out.Error = "出口 IP 检测失败: " + err.Error()
		_ = s.db.Create(&out).Error
		return out, err
	}
	out.IP = ip
	out.TrustScore = s.trustScore(ctx, ip)

	region, err := s.googleRegion(ctx)
	if err != nil {
		out.Error = "Google 区域检测失败: " + err.Error()
		out.LatencyMs = time.Since(start).Milliseconds()
		_ = s.db.Create(&out).Error
		return out, err
	}
	out.GoogleRegion = region
	out.IsCN = region == "CN"
	out.LatencyMs = time.Since(start).Milliseconds()
	if err := s.db.Create(&out).Error; err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) tick(ctx context.Context) {
	settings, err := s.GetSettings()
	if err != nil || settings.DetectMode == "manual" {
		return
	}
	now := time.Now().Unix()
	if settings.NextCheckAt == 0 {
		s.setNextCheckAt(now + randomInterval(int64(settings.DetectIntervalSeconds)))
		return
	}
	if now < settings.NextCheckAt {
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.Check(runCtx)
	if err == nil && res.IsCN {
		s.MaybeAutoWarm(ctx, res)
	}

	next := now + randomInterval(int64(settings.DetectIntervalSeconds))
	// 人不会每轮都出现，固定概率跳过一整轮。
	if rand.Intn(100) < 20 {
		next += int64(settings.DetectIntervalSeconds)
	}
	s.setNextCheckAt(next)
}

func randomInterval(seconds int64) int64 {
	if seconds < 1200 {
		return seconds
	}
	jitter := seconds / 5
	return seconds + int64(rand.Intn(int(2*jitter+1))) - jitter
}

func (s *Service) setNextCheckAt(next int64) {
	_ = s.db.Model(&model.IPCareSettings{}).Where("id = ?", 1).
		Update("next_check_at", next).Error
}

// MaybeAutoWarm 在用户开启自动纠偏且检测为 CN 时执行一次温养。
func (s *Service) MaybeAutoWarm(ctx context.Context, check model.IPCareCheck) {
	if !check.IsCN {
		return
	}
	settings, err := s.GetSettings()
	if err != nil || settings.WarmMode != "auto" {
		return
	}
	warmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_, _ = s.Warm(warmCtx)
}

func (s *Service) publicIP(ctx context.Context) (string, error) {
	urls := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://ipinfo.io/ip",
	}
	var lastErr error
	for _, u := range urls {
		body, err := s.get(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip, nil
		}
		lastErr = fmt.Errorf("%s 返回的不是 IP", u)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的出口 IP 接口")
	}
	return "", lastErr
}

func (s *Service) googleRegion(ctx context.Context) (string, error) {
	urls := []string{
		"https://www.youtube.com/premium",
		"https://music.youtube.com/",
	}

	type result struct {
		region string
		err    error
	}
	ch := make(chan result, len(urls))
	for _, u := range urls {
		go func(u string) {
			body, err := s.get(ctx, u)
			if err != nil {
				ch <- result{err: err}
				return
			}
			if bytes.Contains(body, []byte("www.google.cn")) {
				ch <- result{region: "CN"}
				return
			}
			m := regionRe.FindSubmatch(body)
			if len(m) < 2 {
				ch <- result{err: fmt.Errorf("%s 未返回区域信息", u)}
				return
			}
			ch <- result{region: strings.ToUpper(string(m[1]))}
		}(u)
	}

	var regions []string
	var errs []string
	for range urls {
		r := <-ch
		if r.err != nil {
			errs = append(errs, r.err.Error())
			continue
		}
		if r.region != "" {
			regions = append(regions, r.region)
		}
	}
	for _, r := range regions {
		if r == "CN" {
			return "CN", nil
		}
	}
	if len(regions) > 0 {
		return regions[0], nil
	}
	if len(errs) == 0 {
		errs = append(errs, "没有返回区域信息")
	}
	return "", fmt.Errorf("%s", strings.Join(errs, "; "))
}

func (s *Service) trustScore(ctx context.Context, ip string) int {
	if ip == "" {
		return -1
	}
	body, err := s.get(ctx, "https://api.ipapi.is/?q="+url.QueryEscape(ip))
	if err != nil {
		return -1
	}
	var info struct {
		IsAbuser     bool `json:"is_abuser"`
		IsCrawler    bool `json:"is_crawler"`
		IsDatacenter bool `json:"is_datacenter"`
		IsVPN        bool `json:"is_vpn"`
		IsTor        bool `json:"is_tor"`
		IsProxy      bool `json:"is_proxy"`
		AbuseScore   int  `json:"abuse_confidence_score"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return -1
	}
	score := 100
	if info.AbuseScore > 0 {
		score -= info.AbuseScore
	} else if info.IsAbuser {
		score -= 50
	}
	if info.IsProxy {
		score -= 15
	}
	if info.IsVPN {
		score -= 15
	}
	if info.IsTor {
		score -= 30
	}
	if info.IsCrawler {
		score -= 10
	}
	if info.IsDatacenter {
		score -= 10
	}
	if score < 0 {
		score = 0
	}
	return score
}

func (s *Service) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", probeUA)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
