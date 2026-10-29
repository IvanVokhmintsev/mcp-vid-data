package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// runServe — HTTP-прокси MCP-клиента к HTTP-сервису 1С.
// Клиент ходит на Listen+Path (по умолчанию :8787/mcp); прокси добавляет
// Basic-auth и пересылает запрос в Upstream. STDIO-транспорт не используется.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "путь к файлу конфигурации")
	listen := fs.String("listen", "", "адрес слушателя (переопределяет конфиг / RSV_LISTEN)")
	upstream := fs.String("upstream", "", "URL HTTP-сервиса 1С (переопределяет конфиг / RSV_UPSTREAM)")
	_ = fs.Parse(args)

	cfg, err := resolveConfig(*configPath, "")
	if err != nil {
		// В Docker часто нет файла — достаточно переменных окружения.
		cfg = &Config{}
	}
	cfg.applyEnv()
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *upstream != "" {
		cfg.Upstream = *upstream
	}

	if !cfg.HasHTTPUpstream() {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge: не задан upstream (RSV_UPSTREAM, --upstream или поле upstream в конфиге)")
		os.Exit(1)
	}

	proxy := &httpProxy{cfg: cfg, client: &http.Client{Timeout: 120 * time.Second}}
	mux := http.NewServeMux()
	path := cfg.MCPPath()
	mux.HandleFunc(path, proxy.handle)
	// Частый вариант: клиент бьёт в корень — тоже принимаем.
	if path != "/" {
		mux.HandleFunc("/", proxy.handle)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	addr := cfg.ListenAddr()
	srv := &http.Server{
		Addr:              addr,
		Handler:           loggingHandler(mux),
		ReadHeaderTimeout: 15 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge: не удалось слушать", addr+":", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "rsvdata-bridge: HTTP-прокси слушает %s%s → %s\n",
		displayListen(addr), path, cfg.UpstreamURL())

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "rsvdata-bridge: сервер остановлен:", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func displayListen(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}

type httpProxy struct {
	cfg    *Config
	client *http.Client
}

func (p *httpProxy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, Mcp-Session-Id")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		http.Error(w, "не удалось прочитать тело запроса", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	out, status, err := p.forward(r.Context(), r.Method, r.Header, body)
	if err != nil {
		log.Printf("upstream error: %v", err)
		writeJSONError(w, extractID(body), -32000, "Прокси 1С: "+err.Error())
		return
	}
	if status == 0 {
		status = http.StatusOK
	}

	// Уведомления MCP без id могут вернуть пустое тело — отдаём 202.
	if len(bytes.TrimSpace(out)) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(compactJSON(out))
	if len(out) > 0 && out[len(out)-1] != '\n' {
		_, _ = w.Write([]byte{'\n'})
	}
}

func (p *httpProxy) forward(ctx context.Context, method string, hdr http.Header, body []byte) ([]byte, int, error) {
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, p.cfg.UpstreamURL(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}

	// Пробрасываем заголовки протокола MCP; Authorization всегда наш (к 1С).
	if ct := hdr.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	} else if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept := hdr.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if sid := hdr.Get("Mcp-Session-Id"); sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	if auth := p.cfg.BasicAuth(); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return data, resp.StatusCode, fmt.Errorf("HTTP 401 — неверный логин или пароль к 1С")
	}
	if resp.StatusCode >= 400 {
		return data, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, resp.StatusCode, nil
}

func compactJSON(in []byte) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, in); err == nil {
		return buf.Bytes()
	}
	return bytes.ReplaceAll(bytes.ReplaceAll(in, []byte("\r"), nil), []byte("\n"), nil)
}

func writeJSONError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK) // JSON-RPC ошибки — в теле, HTTP 200
	_, _ = io.WriteString(w, errorResponse(id, code, message)+"\n")
}

func extractID(reqLine []byte) json.RawMessage {
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(reqLine, &m)
	return m.ID
}

func errorResponse(id json.RawMessage, code int, message string) string {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	payload := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: id}
	payload.Error.Code = code
	payload.Error.Message = message
	b, _ := json.Marshal(payload)
	return string(b)
}

func loggingHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
