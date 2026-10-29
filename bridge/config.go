package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config — настройки HTTP-прокси к MCP HTTP-сервису 1С.
// Хранится в JSON или задаётся переменными окружения (удобно для Docker).
// Логин/пароль — обычным текстом (аутентификация 1С включена).
type Config struct {
	// Listen — адрес HTTP-слушателя прокси, например ":8787" или "0.0.0.0:8787".
	Listen string `json:"listen,omitempty"`

	// Upstream — URL MCP HTTP-сервиса 1С
	// (обычно http://<сервер>/<публикация>/hs/rsvdata/mcp).
	Upstream string `json:"upstream,omitempty"`

	// User / Password — учётные данные для Basic-auth к Upstream.
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`

	// Path — путь, на котором прокси принимает MCP (по умолчанию "/mcp").
	Path string `json:"path,omitempty"`

	// --- устаревшие поля COM-моста (Windows); оставлены для совместимости конфигов ---

	// Connect — полная строка соединения 1С (COM). Если задана, перекрывает структурированные поля.
	Connect string `json:"connect,omitempty"`

	Kind   string `json:"kind,omitempty"`   // "file" или "server"
	File   string `json:"file,omitempty"`   // путь к папке файловой базы
	Server string `json:"server,omitempty"` // адрес сервера 1С
	Ref    string `json:"ref,omitempty"`    // имя базы на сервере
}

const (
	defaultListen = ":8787"
	defaultPath   = "/mcp"
)

// ListenAddr возвращает адрес слушателя с подстановкой значения по умолчанию.
func (c *Config) ListenAddr() string {
	if strings.TrimSpace(c.Listen) != "" {
		return c.Listen
	}
	return defaultListen
}

// MCPPath возвращает путь MCP-эндпоинта прокси.
func (c *Config) MCPPath() string {
	p := strings.TrimSpace(c.Path)
	if p == "" {
		return defaultPath
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// UpstreamURL возвращает URL HTTP-сервиса 1С.
func (c *Config) UpstreamURL() string {
	return strings.TrimSpace(c.Upstream)
}

// BasicAuth возвращает значение заголовка Authorization для Upstream (или пустую строку).
func (c *Config) BasicAuth() string {
	if strings.TrimSpace(c.User) == "" && strings.TrimSpace(c.Password) == "" {
		return ""
	}
	return buildBasicAuth(c.User, c.Password)
}

// ConnectString собирает строку соединения для V8*.COMConnector.Connect (диагностика Windows).
func (c *Config) ConnectString() string {
	if strings.TrimSpace(c.Connect) != "" {
		return c.Connect
	}
	var sb strings.Builder
	if c.Kind == "server" {
		fmt.Fprintf(&sb, `Srvr="%s";Ref="%s";`, c.Server, c.Ref)
	} else if strings.TrimSpace(c.File) != "" {
		fmt.Fprintf(&sb, `File="%s";`, c.File)
	} else {
		return ""
	}
	if strings.TrimSpace(c.User) != "" {
		fmt.Fprintf(&sb, `Usr="%s";Pwd="%s";`, c.User, c.Password)
	}
	return sb.String()
}

// HasHTTPUpstream — конфиг готов для HTTP-прокси / HTTP-ping.
func (c *Config) HasHTTPUpstream() bool {
	return c.UpstreamURL() != ""
}

// HasCOMConnect — конфиг содержит параметры COM (Windows).
func (c *Config) HasCOMConnect() bool {
	return strings.TrimSpace(c.ConnectString()) != ""
}

// applyEnv перекрывает поля конфига переменными окружения (Docker / systemd).
// Пустые переменные не трогают уже заданные значения.
func (c *Config) applyEnv() {
	if v := strings.TrimSpace(os.Getenv("RSV_LISTEN")); v != "" {
		c.Listen = v
	}
	if v := strings.TrimSpace(os.Getenv("RSV_UPSTREAM")); v != "" {
		c.Upstream = v
	}
	if v := os.Getenv("RSV_USER"); v != "" {
		c.User = v
	}
	if v, ok := os.LookupEnv("RSV_PASSWORD"); ok {
		c.Password = v
	}
	if v := strings.TrimSpace(os.Getenv("RSV_PATH")); v != "" {
		c.Path = v
	}
}

// configDir — папка с конфигами: $XDG_CONFIG_HOME/MCP-RSV-Data, иначе
// %APPDATA%\MCP-RSV-Data (Windows) или ~/.config/MCP-RSV-Data.
func configDir() string {
	if dir := os.Getenv("RSV_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "MCP-RSV-Data")
	}
	if dir := os.Getenv("APPDATA"); dir != "" {
		return filepath.Join(dir, "MCP-RSV-Data")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "MCP-RSV-Data")
	}
	return "MCP-RSV-Data"
}

// defaultConfigPath — конфиг по умолчанию (если имя подключения не задано).
func defaultConfigPath() string {
	return filepath.Join(configDir(), "bridge.json")
}

// namedConfigPath — отдельный конфиг для подключения с данным именем.
func namedConfigPath(name string) string {
	return filepath.Join(configDir(), sanitizeFileName(name)+".json")
}

// sanitizeFileName убирает из имени символы, недопустимые в имени файла.
func sanitizeFileName(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}
	out := strings.Map(repl, strings.TrimSpace(s))
	if out == "" {
		return "rsv-data"
	}
	return out
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("неверный формат JSON: %w", err)
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
