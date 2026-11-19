package llm

import (
	"net"
	"net/http"
	"time"
)

// newHTTPClient returns an http.Client tuned for LLM API calls.
//
// No overall Client.Timeout is set because streaming responses legitimately
// take many minutes; instead we bound connection setup and time-to-first-byte
// so a dead endpoint cannot hang a turn forever.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 180 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          10,
			MaxIdleConnsPerHost:   4,
		},
	}
}
