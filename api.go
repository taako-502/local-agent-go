package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

var orderID = regexp.MustCompile(`^ORD-[0-9]{3}$`)

// GetOrder exposes one read-only business operation, not arbitrary HTTP access.
func (a *Activities) GetOrder(ctx context.Context, d Decision) (string, error) {
	if !orderID.MatchString(d.OrderID) {
		return "Tool error: order_id must match ORD-001", nil
	}
	u, err := url.Parse(a.APIBase)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "Tool error: sample API must be a loopback HTTP origin", nil
	}
	u.Path = "/orders/" + d.OrderID
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

type Order struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Item             string `json:"item"`
	Quantity         int    `json:"quantity"`
	ExpectedDelivery string `json:"expected_delivery"`
}

func sampleAPI() http.Handler {
	orders := map[string]Order{
		"ORD-001": {"ORD-001", "shipped", "キーボード", 1, "2026-11-02"},
		"ORD-002": {"ORD-002", "processing", "モニター", 2, "2026-11-05"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		order, ok := orders[r.PathValue("id")]
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "order not found"})
			return
		}
		json.NewEncoder(w).Encode(order)
	})
	return mux
}
