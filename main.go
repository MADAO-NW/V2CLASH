package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	maxBodyBytes = 200 * 1024
	maxTokens    = 500
)

type ConvertRequest struct {
	Input string `json:"input"`
}

type ConvertError struct {
	Index   int    `json:"index"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

type ConvertResponse struct {
	ProxyLines string         `json:"proxy_lines"`
	GroupLines string         `json:"group_lines"`
	Errors     []ConvertError `json:"errors"`
}

type Proxy struct {
	Name    string
	Type    string
	Server  string
	Port    int
	UUID    string
	AlterID int
	Cipher  string
	Network string
	TLS     bool
	UDP     bool
}

func main() {
	addr := "127.0.0.1:7625"
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		addr = "127.0.0.1:" + p
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/convert", convertHandler)
	staticDir := "./static"
	if _, err := os.Stat("./dist/static"); err == nil {
		staticDir = "./dist/static"
	}
	mux.Handle("/", http.FileServer(http.Dir(staticDir)))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("link2clash listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func convertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	var req ConvertRequest
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(req.Input) > maxBodyBytes {
		writeAPIError(w, http.StatusBadRequest, "input too large")
		return
	}

	tokens := splitTokens(req.Input)
	if len(tokens) > maxTokens {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("too many items (max %d)", maxTokens))
		return
	}

	proxies := make([]string, 0, len(tokens))
	groups := make([]string, 0, len(tokens))
	errs := make([]ConvertError, 0)

	for i, token := range tokens {
		var (
			proxy Proxy
			err   error
		)

		switch {
		case strings.HasPrefix(token, "vless://"):
			proxy, err = parseVless(token)
		case strings.HasPrefix(token, "vmess://"):
			proxy, err = parseVmess(token)
		default:
			err = fmt.Errorf("unsupported scheme")
		}

		if err != nil {
			errs = append(errs, ConvertError{
				Index:   i + 1,
				Value:   token,
				Message: err.Error(),
			})
			continue
		}

		proxies = append(proxies, formatProxyLine(proxy))
		groups = append(groups, formatGroupLine(proxy.Name))
	}

	resp := ConvertResponse{
		ProxyLines: strings.Join(proxies, "\n"),
		GroupLines: strings.Join(groups, "\n"),
		Errors:     errs,
	}
	writeJSON(w, resp)
}

func splitTokens(input string) []string {
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '，'
	})

	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func parseVless(raw string) (Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid vless URL")
	}
	if u.Scheme != "vless" {
		return Proxy{}, fmt.Errorf("invalid vless scheme")
	}

	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	if uuid == "" {
		return Proxy{}, fmt.Errorf("missing uuid")
	}

	server := u.Hostname()
	if server == "" {
		return Proxy{}, fmt.Errorf("missing server")
	}

	portStr := u.Port()
	if portStr == "" {
		return Proxy{}, fmt.Errorf("missing port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid port")
	}

	q := u.Query()
	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}

	tls := false
	security := q.Get("security")
	if security == "tls" || security == "reality" {
		tls = true
	}

	cipher := "none"
	if enc := q.Get("encryption"); enc != "" {
		cipher = enc
	}

	name := ""
	if u.Fragment != "" {
		decoded, err := url.PathUnescape(u.Fragment)
		if err == nil {
			name = decoded
		} else {
			name = u.Fragment
		}
	}
	if name == "" {
		name = fmt.Sprintf("vless-%s-%d", server, port)
	}

	return Proxy{
		Name:    name,
		Type:    "vless",
		Server:  server,
		Port:    port,
		UUID:    uuid,
		Cipher:  cipher,
		Network: network,
		TLS:     tls,
		UDP:     true,
	}, nil
}

func parseVmess(raw string) (Proxy, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	if payload == "" {
		return Proxy{}, fmt.Errorf("empty vmess payload")
	}

	decoded, err := decodeBase64Compat(payload)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid vmess base64")
	}

	var m map[string]interface{}
	if err := json.Unmarshal(decoded, &m); err != nil {
		return Proxy{}, fmt.Errorf("invalid vmess JSON")
	}

	server := strings.TrimSpace(getString(m, "add"))
	portStr := strings.TrimSpace(getString(m, "port"))
	uuid := strings.TrimSpace(getString(m, "id"))
	if server == "" || portStr == "" || uuid == "" {
		return Proxy{}, fmt.Errorf("missing required vmess fields")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid port")
	}

	alterID := 0
	if aid := strings.TrimSpace(getString(m, "aid")); aid != "" {
		val, err := strconv.Atoi(aid)
		if err != nil {
			return Proxy{}, fmt.Errorf("invalid alterId")
		}
		alterID = val
	}

	network := strings.TrimSpace(getString(m, "net"))
	if network == "" {
		network = "tcp"
	}

	tls := false
	if strings.EqualFold(strings.TrimSpace(getString(m, "tls")), "tls") {
		tls = true
	}

	cipher := strings.TrimSpace(getString(m, "scy"))
	if cipher == "" {
		cipher = "auto"
	}

	name := strings.TrimSpace(getString(m, "ps"))
	if name == "" {
		name = fmt.Sprintf("vmess-%s-%d", server, port)
	}

	return Proxy{
		Name:    name,
		Type:    "vmess",
		Server:  server,
		Port:    port,
		UUID:    uuid,
		AlterID: alterID,
		Cipher:  cipher,
		Network: network,
		TLS:     tls,
		UDP:     true,
	}, nil
}

func decodeBase64Compat(input string) ([]byte, error) {
	trimmed := strings.TrimSpace(input)
	padded := padBase64(trimmed)

	if b, err := base64.StdEncoding.DecodeString(padded); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(padded); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(trimmed)
}

func padBase64(input string) string {
	if mod := len(input) % 4; mod != 0 {
		return input + strings.Repeat("=", 4-mod)
	}
	return input
}

func getString(m map[string]interface{}, key string) string {
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatProxyLine(p Proxy) string {
	if p.Type == "vmess" {
		return fmt.Sprintf("- {name: %s, type: vmess, server: %s, port: %d, uuid: %s, alterId: %d, cipher: %s, network: %s, tls: %t, udp: true}",
			p.Name, p.Server, p.Port, p.UUID, p.AlterID, p.Cipher, p.Network, p.TLS)
	}
	return fmt.Sprintf("- {name: %s, type: vless, server: %s, port: %d, uuid: %s, cipher: %s, network: %s, tls: %t, udp: true}",
		p.Name, p.Server, p.Port, p.UUID, p.Cipher, p.Network, p.TLS)
}

func formatGroupLine(name string) string {
	return "- " + strconv.Quote(name)
}

func writeJSON(w http.ResponseWriter, resp ConvertResponse) {
	if resp.Errors == nil {
		resp.Errors = []ConvertError{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
