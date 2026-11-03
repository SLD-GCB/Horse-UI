package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// MasqueradeConfig decides what an unauthenticated visitor sees.
type MasqueradeConfig struct {
	Type       string `json:"type"` // "", "404", "string", "file", "proxy"
	Content    string `json:"content"`
	Root       string `json:"root"`
	ProxyURL   string `json:"proxy_url"`
	StatusCode int    `json:"status_code"`
}

// NewMasquerade builds the handler for unauthenticated traffic.
func NewMasquerade(config MasqueradeConfig) (http.Handler, error) {
	switch strings.ToLower(config.Type) {
	case "", "404":
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte("<!doctype html><title>404</title><h1>Not Found</h1>"))
		}), nil
	case "string":
		status := config.StatusCode
		if status == 0 {
			status = http.StatusOK
		}
		content := config.Content
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(content))
		}), nil
	case "file":
		if config.Root == "" {
			return nil, fmt.Errorf("masquerade: root is required for type file")
		}
		return http.FileServer(http.Dir(config.Root)), nil
	case "proxy":
		target, err := url.Parse(config.ProxyURL)
		if err != nil || target.Host == "" {
			return nil, fmt.Errorf("masquerade: bad proxy_url %q", config.ProxyURL)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		originalDirector := proxy.Director
		proxy.Director = func(request *http.Request) {
			originalDirector(request)
			request.Host = target.Host
		}
		return proxy, nil
	default:
		return nil, fmt.Errorf("masquerade: unknown type %q", config.Type)
	}
}
