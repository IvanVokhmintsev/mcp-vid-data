# MCP:RSV Data — HTTP-прокси (Go)

> Посредник между MCP-клиентом (Claude, Cursor и др.) и HTTP-сервисом расширения
> RSVData в базе 1С. **Основной способ запуска — Docker на Linux.**
> STDIO-транспорт не используется. Лицензия — MIT.

## Зачем прокси

MCP-клиент умеет ходить по **HTTP** (`http` / `streamable-http`), но креды к 1С
удобнее держать на стороне прокси (файл конфига или переменные окружения Docker),
а не в `.mcp.json` каждого клиента:

```
MCP-клиент  ──HTTP (JSON-RPC)──►  rsvdata-bridge  ──HTTP + Basic──►  1С /hs/rsvdata/mcp
```

Что это даёт:

- **Без STDIO.** Один и тот же HTTP-транспорт у Claude Code, Cursor и Cline.
- **Креды не в клиенте.** В `.mcp.json` только URL прокси.
- **Linux / Docker first.** Образ собирается под Linux amd64; на машине агента
  платформа 1С и COM не нужны.

База 1С при этом **должна быть опубликована** с HTTP-сервисом расширения RSVData.

## Сторона 1С

Достаточно установленного расширения RSVData и публикации HTTP-сервиса
`…/hs/rsvdata/mcp` (как в способе «напрямую к HTTP-сервису» в [`guide.md`](../guide.md)).

## Сборка бинарника

Нужен Go 1.22+.

```bash
cd bridge
go build -o rsvdata-bridge .
```

## Docker (рекомендуется)

```bash
cd bridge
docker build -t rsvdata-bridge .

docker run --rm -p 8787:8787 \
  -e RSV_UPSTREAM='http://host.docker.internal/UT_Demo/hs/rsvdata/mcp' \
  -e RSV_USER='Администратор' \
  -e RSV_PASSWORD='' \
  rsvdata-bridge
```

Или через Compose (см. `docker-compose.yml`):

```bash
RSV_UPSTREAM='http://host.docker.internal/UT_Demo/hs/rsvdata/mcp' \
RSV_USER='Администратор' \
docker compose up --build
```

Проверка живости: `curl http://127.0.0.1:8787/healthz` → `ok`.

Конфиг клиента (Cursor):

```json
{
  "mcpServers": {
    "rsv-data": {
      "type": "streamable-http",
      "url": "http://127.0.0.1:8787/mcp"
    }
  }
}
```

## Переменные окружения

| Переменная | Назначение | По умолчанию |
|---|---|---|
| `RSV_LISTEN` | адрес слушателя | `:8787` |
| `RSV_UPSTREAM` | URL HTTP-сервиса 1С | — (обязательно) |
| `RSV_USER` | логин ИБ | |
| `RSV_PASSWORD` | пароль ИБ | |
| `RSV_PATH` | путь MCP на прокси | `/mcp` |
| `RSV_CONFIG_DIR` | каталог файлов конфига | см. ниже |

## Подключение для пользователя (мастер)

```bash
rsvdata-bridge setup
```

- **[1] Напрямую к HTTP-сервису 1С** — клиент ходит в базу сам; прокси не нужен.
  Мастер собирает `Authorization: Basic …` и правильный `type` транспорта.
- **[2] Через HTTP-прокси** — спрашивает listen/upstream/логин/пароль, проверяет
  связь, сохраняет `%конфиг%/MCP-RSV-Data/<имя>.json`, печатает блок `.mcp.json`
  с URL прокси **без** пароля.

## Команды

| Команда | Назначение |
|---|---|
| `rsvdata-bridge` (TTY) | Мастер настройки. |
| `rsvdata-bridge` (без TTY) | `serve` — удобно для Docker/`CMD`. |
| `rsvdata-bridge serve` | HTTP-прокси. |
| `rsvdata-bridge setup` | Мастер явно. |
| `rsvdata-bridge ping` | Проверка upstream по HTTP (или COM на Windows). |
| `rsvdata-bridge diag` | Список расширений через COM (только Windows). |

## Как устроено (для разработчика)

- `main.go` — разбор подкоманды; без TTY по умолчанию `serve`.
- `config.go` — конфиг + `RSV_*` env; пути Linux (`~/.config`) и Windows (`%APPDATA%`).
- `serve.go` — HTTP-слушатель, проксирование в upstream, `/healthz`.
- `setup.go` — мастер (прямой HTTP / прокси) + `ping`.
- `onec.go` / `diag.go` — COM (Windows), для диагностики старых сценариев.

## Статус

✅ HTTP-прокси + Docker под Linux.
⬜ Дальше — по мере поступления фич.
