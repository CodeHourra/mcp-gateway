package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	releaseAPI      = "https://api.github.com/repos/CodeHourra/mcp-gateway/releases/latest"
	releaseDownload = "https://github.com/CodeHourra/mcp-gateway/releases/download"
	maxUpdateSize   = 512 << 20
)

var semver = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// Updater finds and downloads the newest published release. It never installs:
// replacing a running, unsigned app is left to the user, who gets a verified file.
type Updater struct {
	API          string
	DownloadBase string
	// Suffix is the release asset ending for this platform, e.g. "arm64.dmg".
	Suffix string
	Client *http.Client
}

type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Asset     string `json:"asset,omitempty"`
	Message   string `json:"message"`
}

func newUpdater() Updater {
	u := Updater{API: releaseAPI, DownloadBase: releaseDownload, Client: &http.Client{}}
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		u.Suffix = "arm64.dmg"
	case "windows/amd64":
		u.Suffix = "windows-amd64.zip"
	}
	return u
}

func parseSemver(v string) ([3]int, error) {
	var out [3]int
	m := semver.FindStringSubmatch(v)
	if m == nil {
		return out, fmt.Errorf("版本号格式无效: %q", v)
	}
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return out, err
		}
		out[i] = n
	}
	return out, nil
}

func lessVersion(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func (u Updater) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mcp-gateway-updater")
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接发布源: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("发布源返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// Check compares current with the latest published, non-prerelease version.
func (u Updater) Check(ctx context.Context, current string) (UpdateInfo, error) {
	cur, err := parseSemver(current)
	if err != nil {
		return UpdateInfo{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := u.get(ctx, u.API)
	if err != nil {
		return UpdateInfo{}, err
	}
	defer resp.Body.Close()
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return UpdateInfo{}, fmt.Errorf("发布信息无法解析: %w", err)
	}
	latest := strings.TrimPrefix(release.Tag, "v")
	lat, err := parseSemver(latest)
	if err != nil {
		return UpdateInfo{}, err
	}
	info := UpdateInfo{Current: current, Latest: latest}
	if lat == cur || lessVersion(lat, cur) {
		info.Message = "当前 " + current + " 已是最新版本。"
		return info, nil
	}
	if u.Suffix == "" {
		info.Message = "发现新版本 " + latest + "，但本机平台没有对应的安装包，请前往发布页手动获取。"
		return info, nil
	}
	asset := "MCP-Gateway-" + latest + "-" + u.Suffix
	for _, a := range release.Assets {
		if a.Name == asset {
			info.Available, info.Asset = true, asset
			info.Message = "发现新版本 " + latest + "（当前 " + current + "）。"
			return info, nil
		}
	}
	info.Message = "新版本 " + latest + " 已发布，但安装包尚未就绪，稍后再试。"
	return info, nil
}

// Download fetches the installer for the newest version into dir and returns its
// path. The file only appears under its final name after its SHA-256 matches the
// release's .sha256 file; dir is emptied first so old installers do not pile up.
func (u Updater) Download(ctx context.Context, current, dir string) (string, UpdateInfo, error) {
	info, err := u.Check(ctx, current)
	if err != nil {
		return "", info, err
	}
	if !info.Available {
		return "", info, errors.New(info.Message)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	assetURL := u.DownloadBase + "/v" + info.Latest + "/" + info.Asset
	want, err := u.fetchChecksum(ctx, assetURL+".sha256", info.Asset)
	if err != nil {
		return "", info, err
	}
	resp, err := u.get(ctx, assetURL)
	if err != nil {
		return "", info, err
	}
	defer resp.Body.Close()
	if err := os.RemoveAll(dir); err != nil {
		return "", info, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", info, err
	}
	final := filepath.Join(dir, info.Asset)
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", info, err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, maxUpdateSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		err = fmt.Errorf("下载中断: %w", err)
	case n > maxUpdateSize:
		err = errors.New("安装包超过大小上限，已放弃")
	case hex.EncodeToString(sum.Sum(nil)) != want:
		err = errors.New("安装包 SHA-256 与发布校验值不一致，已丢弃，请勿安装")
	}
	if err != nil {
		os.Remove(part)
		return "", info, err
	}
	if err := os.Rename(part, final); err != nil {
		return "", info, err
	}
	return final, info, nil
}

// fetchChecksum reads "<hex>  <name>" as written by shasum/sha256sum.
func (u Updater) fetchChecksum(ctx context.Context, url, name string) (string, error) {
	resp, err := u.get(ctx, url)
	if err != nil {
		return "", fmt.Errorf("读取校验文件失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", errors.New("校验文件格式无效")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("校验文件格式无效")
	}
	if len(fields) > 1 && strings.TrimPrefix(fields[1], "*") != name {
		return "", errors.New("校验文件对应的不是该安装包")
	}
	return strings.ToLower(fields[0]), nil
}
