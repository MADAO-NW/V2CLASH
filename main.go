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
	Name           string
	Type           string
	Server         string
	Port           int
	UUID           string
	AlterID        int
	Cipher         string
	Network        string
	TLS            bool
	UDP            bool
	Password       string            // SS/Trojan/Hysteria2 specific
	Plugin         string            // SS obfs plugin
	PluginOpts     map[string]string // SS plugin options
	SNI            string            // Trojan/Hysteria2 specific
	SkipCertVerify bool              // Trojan/Hysteria2 specific
	Obfs           string            // Hysteria2 obfs type
	ObfsPassword   string            // Hysteria2 obfs password
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
		case strings.HasPrefix(token, "ss://"):
			proxy, err = parseSS(token)
		case strings.HasPrefix(token, "trojan://"):
			proxy, err = parseTrojan(token)
		case strings.HasPrefix(token, "hysteria2://") || strings.HasPrefix(token, "hy2://"):
			proxy, err = parseHysteria2(token)
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

func parseSS(raw string) (Proxy, error) {
	// SS link format: ss://base64(method:password)@server:port?plugin=...#name
	// Or legacy format: ss://base64(method:password@server:port)#name

	u, err := url.Parse(raw)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid ss URL")
	}
	if u.Scheme != "ss" {
		return Proxy{}, fmt.Errorf("invalid ss scheme")
	}

	var method, password, server string
	var port int

	// Try SIP002 format first: base64(method:password)@server:port
	if u.User != nil && u.Host != "" {
		// SIP002 format
		userInfo := u.User.Username()
		decoded, err := decodeBase64Compat(userInfo)
		if err != nil {
			return Proxy{}, fmt.Errorf("invalid ss userinfo base64")
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			return Proxy{}, fmt.Errorf("invalid ss userinfo format")
		}
		method = parts[0]
		password = parts[1]

		server = u.Hostname()
		portStr := u.Port()
		if portStr == "" {
			return Proxy{}, fmt.Errorf("missing port")
		}
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return Proxy{}, fmt.Errorf("invalid port")
		}
	} else {
		// Legacy format: base64(method:password@server:port)
		payload := strings.TrimPrefix(raw, "ss://")
		// Remove fragment if exists
		if idx := strings.Index(payload, "#"); idx != -1 {
			payload = payload[:idx]
		}
		// Remove query if exists
		if idx := strings.Index(payload, "?"); idx != -1 {
			payload = payload[:idx]
		}

		decoded, err := decodeBase64Compat(payload)
		if err != nil {
			return Proxy{}, fmt.Errorf("invalid ss base64")
		}

		// Format: method:password@server:port
		atIdx := strings.LastIndex(string(decoded), "@")
		if atIdx == -1 {
			return Proxy{}, fmt.Errorf("invalid ss format")
		}

		userPart := string(decoded)[:atIdx]
		hostPart := string(decoded)[atIdx+1:]

		parts := strings.SplitN(userPart, ":", 2)
		if len(parts) != 2 {
			return Proxy{}, fmt.Errorf("invalid ss method:password")
		}
		method = parts[0]
		password = parts[1]

		colonIdx := strings.LastIndex(hostPart, ":")
		if colonIdx == -1 {
			return Proxy{}, fmt.Errorf("invalid ss server:port")
		}
		server = hostPart[:colonIdx]
		port, err = strconv.Atoi(hostPart[colonIdx+1:])
		if err != nil {
			return Proxy{}, fmt.Errorf("invalid port")
		}
	}

	if server == "" {
		return Proxy{}, fmt.Errorf("missing server")
	}

	// Parse name from fragment
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
		name = fmt.Sprintf("ss-%s-%d", server, port)
	}

	// Parse plugin options (obfs)
	var plugin string
	pluginOpts := make(map[string]string)

	q := u.Query()
	pluginStr := q.Get("plugin")
	if pluginStr == "" {
		// Try parsing from RawQuery for non-standard format like plugin=obfs-local;obfs=http;...
		rawQuery := u.RawQuery
		if strings.HasPrefix(rawQuery, "plugin=") {
			pluginStr, _ = url.QueryUnescape(strings.TrimPrefix(rawQuery, "plugin="))
		}
	}

	if pluginStr != "" {
		// Parse plugin string: obfs-local;obfs=http;obfs-host=xxx
		pluginParts := strings.Split(pluginStr, ";")
		if len(pluginParts) > 0 {
			pluginType := pluginParts[0]
			// Map obfs-local to obfs for Clash
			if pluginType == "obfs-local" || pluginType == "simple-obfs" {
				plugin = "obfs"
			} else {
				plugin = pluginType
			}

			for _, part := range pluginParts[1:] {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 {
					key := kv[0]
					value := kv[1]
					// Map obfs-* keys to Clash format
					switch key {
					case "obfs":
						pluginOpts["mode"] = value
					case "obfs-host":
						pluginOpts["host"] = value
					default:
						pluginOpts[key] = value
					}
				}
			}
		}
	}

	return Proxy{
		Name:       name,
		Type:       "ss",
		Server:     server,
		Port:       port,
		Cipher:     method,
		Password:   password,
		Plugin:     plugin,
		PluginOpts: pluginOpts,
		UDP:        true,
	}, nil
}

func parseTrojan(raw string) (Proxy, error) {
	// Trojan link format: trojan://password@server:port?security=tls&type=tcp&sni=example.com#name
	u, err := url.Parse(raw)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid trojan URL")
	}
	if u.Scheme != "trojan" {
		return Proxy{}, fmt.Errorf("invalid trojan scheme")
	}

	// Password is in the user info
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	if password == "" {
		return Proxy{}, fmt.Errorf("missing password")
	}

	server := u.Hostname()
	if server == "" {
		return Proxy{}, fmt.Errorf("missing server")
	}

	portStr := u.Port()
	if portStr == "" {
		portStr = "443" // Default port for Trojan
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid port")
	}

	q := u.Query()

	// Parse SNI
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer") // Alternative parameter name
	}
	if sni == "" {
		sni = server // Default to server if no SNI specified
	}

	// Parse skip-cert-verify
	skipCertVerify := false
	if allowInsecure := q.Get("allowInsecure"); allowInsecure == "1" || strings.EqualFold(allowInsecure, "true") {
		skipCertVerify = true
	}

	// Parse name from fragment
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
		name = fmt.Sprintf("trojan-%s-%d", server, port)
	}

	return Proxy{
		Name:           name,
		Type:           "trojan",
		Server:         server,
		Port:           port,
		Password:       password,
		SNI:            sni,
		SkipCertVerify: skipCertVerify,
		UDP:            true,
	}, nil
}

func parseHysteria2(raw string) (Proxy, error) {
	// Hysteria2 link format: hysteria2://auth@server:port?sni=example.com&obfs=salamander&obfs-password=xxx#name
	// Also supports: hy2://auth@server:port?...

	u, err := url.Parse(raw)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid hysteria2 URL")
	}
	if u.Scheme != "hysteria2" && u.Scheme != "hy2" {
		return Proxy{}, fmt.Errorf("invalid hysteria2 scheme")
	}

	// Auth/password is in the user info
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	if password == "" {
		return Proxy{}, fmt.Errorf("missing auth/password")
	}

	server := u.Hostname()
	if server == "" {
		return Proxy{}, fmt.Errorf("missing server")
	}

	portStr := u.Port()
	if portStr == "" {
		portStr = "443" // Default port
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid port")
	}

	q := u.Query()

	// Parse SNI
	sni := q.Get("sni")
	if sni == "" {
		sni = server
	}

	// Parse skip-cert-verify
	skipCertVerify := false
	if insecure := q.Get("insecure"); insecure == "1" || strings.EqualFold(insecure, "true") {
		skipCertVerify = true
	}

	// Parse obfs
	obfs := q.Get("obfs")
	obfsPassword := q.Get("obfs-password")

	// Parse name from fragment
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
		name = fmt.Sprintf("hysteria2-%s-%d", server, port)
	}

	return Proxy{
		Name:           name,
		Type:           "hysteria2",
		Server:         server,
		Port:           port,
		Password:       password,
		SNI:            sni,
		SkipCertVerify: skipCertVerify,
		Obfs:           obfs,
		ObfsPassword:   obfsPassword,
		UDP:            true,
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
	switch p.Type {
	case "vmess":
		return fmt.Sprintf("- {name: %s, type: vmess, server: %s, port: %d, uuid: %s, alterId: %d, cipher: %s, network: %s, tls: %t, udp: true}",
			p.Name, p.Server, p.Port, p.UUID, p.AlterID, p.Cipher, p.Network, p.TLS)
	case "ss":
		return formatSSProxyLine(p)
	case "trojan":
		return formatTrojanProxyLine(p)
	case "hysteria2":
		return formatHysteria2ProxyLine(p)
	default:
		return fmt.Sprintf("- {name: %s, type: vless, server: %s, port: %d, uuid: %s, cipher: %s, network: %s, tls: %t, udp: true}",
			p.Name, p.Server, p.Port, p.UUID, p.Cipher, p.Network, p.TLS)
	}
}

func formatSSProxyLine(p Proxy) string {
	base := fmt.Sprintf("- {name: %s, type: ss, server: %s, port: %d, cipher: %s, password: %s, udp: true",
		p.Name, p.Server, p.Port, p.Cipher, p.Password)

	if p.Plugin != "" && len(p.PluginOpts) > 0 {
		base += fmt.Sprintf(", plugin: %s, plugin-opts: {", p.Plugin)
		opts := make([]string, 0, len(p.PluginOpts))
		for k, v := range p.PluginOpts {
			opts = append(opts, fmt.Sprintf("%s: %s", k, v))
		}
		base += strings.Join(opts, ", ") + "}"
	}

	return base + "}"
}

func formatTrojanProxyLine(p Proxy) string {
	base := fmt.Sprintf("- {name: %s, type: trojan, server: %s, port: %d, password: %s, udp: true, sni: %s",
		p.Name, p.Server, p.Port, p.Password, p.SNI)

	if p.SkipCertVerify {
		base += ", skip-cert-verify: true"
	}

	return base + "}"
}

func formatHysteria2ProxyLine(p Proxy) string {
	base := fmt.Sprintf("- {name: %s, type: hysteria2, server: %s, port: %d, password: %s, sni: %s",
		p.Name, p.Server, p.Port, p.Password, p.SNI)

	if p.SkipCertVerify {
		base += ", skip-cert-verify: true"
	}

	if p.Obfs != "" {
		base += fmt.Sprintf(", obfs: %s", p.Obfs)
		if p.ObfsPassword != "" {
			base += fmt.Sprintf(", obfs-password: %s", p.ObfsPassword)
		}
	}

	return base + "}"
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
