package client

import (
	"io"
	"net/http"
)

// NewEntry returns the local HTTP CONNECT proxy. Point the system proxy here.
func (c *Client) NewEntry() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			http.Error(writer, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}

		upstream, err := c.Dial(request.Context(), request.Host)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}

		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			upstream.Close()
			http.Error(writer, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		downstream, _, err := hijacker.Hijack()
		if err != nil {
			upstream.Close()
			return
		}

		if _, err := downstream.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			downstream.Close()
			upstream.Close()
			return
		}

		go func() {
			_, _ = io.Copy(upstream, downstream)
			_ = upstream.Close()
		}()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
	})
}
