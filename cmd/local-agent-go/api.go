package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SayHello はローカルのHello World APIを呼び出し、JSONレスポンスを返します。
func (a *Activities) SayHello(ctx context.Context) (string, error) {
	u, err := url.Parse(a.APIBase)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "Tool error: sample API must be a loopback HTTP origin", nil
	}
	u.Path = "/hello"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	hc := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", fmt.Errorf("sample API HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Tool error: sample API HTTP %d", resp.StatusCode), nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil {
		return "", err
	}
	if len(b) > 8192 || !json.Valid(b) {
		return "", errors.New("invalid or oversized API response")
	}
	return string(b), nil
}

// sampleAPI はGET /helloに挨拶のJSONを返すHTTPハンドラーを作成します。
func sampleAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]string{"message": "Hello, World!"})
	})
	return mux
}
