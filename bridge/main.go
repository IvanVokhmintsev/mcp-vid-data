// Command rsvdata-bridge — HTTP-прокси между MCP-клиентом (Claude, Cursor и др.)
// и HTTP-сервисом расширения RSVData в базе 1С. Основной способ запуска — Docker
// на Linux; локальный бинарник тоже поддерживается.
//
// Подкоманды:
//
//	rsvdata-bridge serve      — HTTP-прокси (по умолчанию в контейнере).
//	rsvdata-bridge setup      — мастер настройки: способ подключения и блок .mcp.json.
//	rsvdata-bridge ping       — разовая проверка связи с upstream (для диагностики).
//	rsvdata-bridge diag       — диагностика через COM (только Windows).
//
// Параметры (по приоритету): флаги → переменные окружения RSV_* → файл конфигурации
// (--config или путь по умолчанию).
package main

import (
	"fmt"
	"os"
)

func main() {
	// Подкоманда — первый аргумент без дефиса.
	// Без аргументов: в интерактивном TTY — мастер; иначе (Docker/systemd) — serve.
	cmd := defaultCommand()
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "serve":
		runServe(args)
	case "setup":
		runSetup(args)
	case "ping":
		runPing(args)
	case "diag":
		runDiag(args)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func defaultCommand() string {
	if isInteractive() {
		return "setup"
	}
	return "serve"
}

func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func usage(w *os.File) {
	fmt.Fprint(w, `MCP:RSV Data — HTTP-прокси к MCP-сервису 1С.

Команды:
  serve    HTTP-прокси. Слушает RSV_LISTEN (по умолчанию :8787), пересылает
           запросы в RSV_UPSTREAM (HTTP-сервис 1С). Это режим Docker/Linux.
  setup    Мастер настройки: способ подключения, проверка связи, блок .mcp.json.
  ping     Разовая проверка связи с upstream (HTTP) или COM (Windows).
  diag     Диагностика базы через COM / внешнее соединение (только Windows).

Общие флаги:
  --config <путь>     Файл конфигурации (по умолчанию рядом с профилем пользователя).
  --listen <addr>     Адрес слушателя (serve), напр. :8787.
  --upstream <url>    URL HTTP-сервиса 1С.
  --connect "<стр>"   Строка соединения 1С для COM (ping/diag на Windows).

Переменные окружения (Docker):
  RSV_LISTEN      адрес слушателя (по умолчанию :8787)
  RSV_UPSTREAM    URL http://…/hs/rsvdata/mcp
  RSV_USER        логин ИБ
  RSV_PASSWORD    пароль ИБ
  RSV_PATH        путь MCP на прокси (по умолчанию /mcp)
  RSV_CONFIG_DIR  каталог конфигов

Примеры:
  rsvdata-bridge setup
  rsvdata-bridge serve --upstream http://1c/UT/hs/rsvdata/mcp
  docker run --rm -p 8787:8787 -e RSV_UPSTREAM=… -e RSV_USER=… -e RSV_PASSWORD=… rsvdata-bridge
`)
}

// resolveConfig читает конфиг с учётом флагов --config / --connect и env.
func resolveConfig(configPath, connect string) (*Config, error) {
	if connect != "" {
		cfg := &Config{Connect: connect}
		cfg.applyEnv()
		return cfg, nil
	}
	path := configPath
	if path == "" {
		path = defaultConfigPath()
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать конфиг %s: %w (задайте RSV_UPSTREAM или запустите «rsvdata-bridge setup»)", path, err)
	}
	cfg.applyEnv()
	return cfg, nil
}
