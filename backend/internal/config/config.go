// config 读取服务运行配置，支持通过环境变量覆盖默认值。
package config

import "os"

// Config 保存服务运行所需的配置项，均可通过环境变量覆盖。
type Config struct {
	SQLitePath string
	HTTPAddr   string
	StaticDir  string
}

// getenv 读取环境变量，为空时返回默认值 def。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load 从环境变量读取配置，未设置时使用默认值。
func Load() Config {
	return Config{
		SQLitePath: getenv("WMS_SQLITE_PATH", "wms.db"),
		HTTPAddr:   getenv("WMS_HTTP_ADDR", ":8080"),
		StaticDir:  getenv("WMS_STATIC_DIR", "../frontend/dist"),
	}
}
