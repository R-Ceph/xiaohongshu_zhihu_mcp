package cookies

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

type Cookier interface {
	LoadCookies() ([]byte, error)
	SaveCookies(data []byte) error
	DeleteCookies() error
}

type localCookie struct {
	path string
}

func NewLoadCookie(path string) Cookier {
	if path == "" {
		panic("path is required")
	}

	return &localCookie{
		path: path,
	}
}

// LoadCookies 从文件中加载 cookies。
func (c *localCookie) LoadCookies() ([]byte, error) {

	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cookies from tmp file")
	}

	return data, nil
}

// SaveCookies 保存 cookies 到文件中。
func (c *localCookie) SaveCookies(data []byte) error {
	return os.WriteFile(c.path, data, 0644)
}

// DeleteCookies 删除 cookies 文件。
func (c *localCookie) DeleteCookies() error {
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		// 文件不存在，返回 nil（认为已经删除）
		return nil
	}
	return os.Remove(c.path)
}

// GetCookiesFilePath 获取小红书 cookies 文件路径（向后兼容）。
func GetCookiesFilePath() string {
	return GetCookiesFilePathForPlatform("xhs")
}

// GetCookiesFilePathForPlatform 获取指定平台的 cookies 文件路径。
//
// Args:
//
//	platform: 平台标识，如 "xhs"、"zhihu"
//
// Returns:
//
//	cookies 文件的完整路径
func GetCookiesFilePathForPlatform(platform string) string {
	// 小红书保持向后兼容
	if platform == "xhs" {
		tmpDir := os.TempDir()
		oldPath := filepath.Join(tmpDir, "cookies.json")
		if _, err := os.Stat(oldPath); err == nil {
			return oldPath
		}

		path := os.Getenv("COOKIES_PATH")
		if path == "" {
			path = "cookies.json"
		}
		return path
	}

	// 其他平台：优先环境变量，否则用 cookies_{platform}.json
	envKey := "COOKIES_PATH_" + strings.ToUpper(platform)
	if path := os.Getenv(envKey); path != "" {
		return path
	}
	return fmt.Sprintf("cookies_%s.json", platform)
}
