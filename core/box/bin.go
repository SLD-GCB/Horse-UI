package box

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const (
	latestAPI = "https://api.github.com/repos/SagerNet/sing-box/releases/latest"
	// 官方下载地址模板，可用 {version} {os} {arch} {ext} 占位。
	defaultURLTemplate = "https://github.com/SagerNet/sing-box/releases/download/v{version}/sing-box-{version}-{os}-{arch}{ext}"
)

// Manager 负责 sing-box 内核二进制的下载、版本查询与校验。
type Manager struct {
	dir         string
	urlTemplate string
	mu          sync.Mutex
}

// NewManager 创建内核二进制管理器，dir 为存放内核的目录。
func NewManager(dir string) *Manager { return &Manager{dir: dir} }

// SetURLTemplate 设置自定义下载地址模板（留空使用官方地址）。
// 支持 {version} {os} {arch} {ext} 占位符，用于安装带 with_v2ray_api 的自编译内核。
func (m *Manager) SetURLTemplate(t string) { m.urlTemplate = strings.TrimSpace(t) }

func (m *Manager) downloadURL(version string) string {
	tpl := m.urlTemplate
	if tpl == "" {
		tpl = defaultURLTemplate
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	r := strings.NewReplacer(
		"{version}", version,
		"{os}", runtime.GOOS,
		"{arch}", assetArch(),
		"{ext}", ext,
	)
	return r.Replace(tpl)
}

func binName() string {
	if runtime.GOOS == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

// Path 返回内核二进制路径。
func (m *Manager) Path() string { return filepath.Join(m.dir, binName()) }

// Installed 内核是否已就位。
func (m *Manager) Installed() bool {
	st, err := os.Stat(m.Path())
	return err == nil && !st.IsDir()
}

var versionRe = regexp.MustCompile(`version\s+([0-9]+\.[0-9]+\.[0-9]+[^\s]*)`)

// Version 读取已安装内核版本。
func (m *Manager) Version(ctx context.Context) (string, error) {
	if !m.Installed() {
		return "", fmt.Errorf("sing-box 未安装")
	}
	out, err := exec.CommandContext(ctx, m.Path(), "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("执行 version 失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if mm := versionRe.FindSubmatch(out); mm != nil {
		return string(mm[1]), nil
	}
	return strings.TrimSpace(string(out)), nil
}

// LatestVersion 查询 GitHub 上最新发布版本号（不含 v 前缀）。
func (m *Manager) LatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "HorseUI")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询最新版本失败: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

// Check 校验指定配置文件是否合法，返回内核输出。
func (m *Manager) Check(ctx context.Context, configPath string) (string, error) {
	if !m.Installed() {
		return "", fmt.Errorf("sing-box 未安装")
	}
	out, err := exec.CommandContext(ctx, m.Path(), "check", "-c", configPath).CombinedOutput()
	return string(out), err
}

// SupportsV2RayAPI 探测内核是否带 with_v2ray_api 编译参数。
// 官方发行版不含该模块，只有自编译内核才支持按用户统计。
func (m *Manager) SupportsV2RayAPI(ctx context.Context) bool {
	if !m.Installed() {
		return false
	}
	probe := `{"log":{"level":"error"},"experimental":{"v2ray_api":{"listen":"127.0.0.1:19099","stats":{"enabled":true,"users":["probe"]}}}}`

	f, err := os.CreateTemp(m.dir, "probe-*.json")
	if err != nil {
		return false
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString(probe); err != nil {
		f.Close()
		return false
	}
	if err := f.Close(); err != nil {
		return false
	}

	if _, err := exec.CommandContext(ctx, m.Path(), "check", "-c", name).CombinedOutput(); err != nil {
		return false
	}
	return true
}

func assetArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	case "arm":
		return "armv7"
	default:
		return runtime.GOARCH
	}
}

// Install 下载并安装指定版本内核，原子替换现有二进制。
func (m *Manager) Install(ctx context.Context, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return fmt.Errorf("版本号为空")
	}
	url := m.downloadURL(version)

	tmp, err := os.CreateTemp(m.dir, "sing-box-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	req.Header.Set("User-Agent", "HorseUI")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("下载失败: HTTP %d (%s)", resp.StatusCode, url)
	}
	size, err := io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		return err
	}
	if size < 1024*1024 {
		return fmt.Errorf("下载文件异常（%d 字节）", size)
	}

	staging := filepath.Join(m.dir, ".extract")
	_ = os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		err = extractZip(tmpName, staging)
	} else {
		err = extractTarGz(tmpName, staging)
	}
	if err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	binSrc := filepath.Join(staging, binName())
	if _, err := os.Stat(binSrc); err != nil {
		return fmt.Errorf("归档中未找到 %s", binName())
	}
	if err := os.Chmod(binSrc, 0o755); err != nil {
		return err
	}

	// 归档内除主程序外还可能含运行库（如 Windows 的 libcronet.dll），需一并安装。
	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := replaceFile(filepath.Join(staging, e.Name()), filepath.Join(m.dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// flattenName 去掉归档内的顶层版本目录，返回相对路径。
func flattenName(name string) string {
	parts := strings.Split(strings.ReplaceAll(name, "\\", "/"), "/")
	if len(parts) <= 1 {
		return ""
	}
	rel := strings.Join(parts[1:], "/")
	if rel == "" || strings.Contains(rel, "..") {
		return ""
	}
	return rel
}

func extractZip(archivePath, destDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := flattenName(f.Name)
		if rel == "" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(destDir, filepath.FromSlash(rel)), rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		rel := flattenName(hdr.Name)
		if rel == "" {
			continue
		}
		if err := writeFile(filepath.Join(destDir, filepath.FromSlash(rel)), tr); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(dest string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

// replaceFile 用 src 覆盖 dst（Windows 上运行中的文件需先删除）。
func replaceFile(src, dst string) error {
	_ = os.Remove(dst)
	return os.Rename(src, dst)
}
