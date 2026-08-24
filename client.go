package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	requestTimeout = 15 * time.Second

	maxBody = 4 << 20
)

type client struct {
	hc *http.Client
}

func newClient() *client {
	return &client{hc: &http.Client{Timeout: requestTimeout}}
}

type result struct {
	Status   int
	Body     []byte
	Duration time.Duration
}

func (c *client) postForm(ctx context.Context, target string, values url.Values) (result, error) {
	return c.form(ctx, http.MethodPost, target, values)
}

func (c *client) deleteForm(ctx context.Context, target string, values url.Values) (result, error) {
	return c.form(ctx, http.MethodDelete, target, values)
}

func (c *client) form(ctx context.Context, method, target string, values url.Values) (result, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(values.Encode()))
	if err != nil {
		return result{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.do(req)
}

func (c *client) get(ctx context.Context, target, bearer string) (result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return result{}, err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.do(req)
}

// do runs the request. A non-2xx response is not an error here; the callers
// decide what each status means, because the token endpoint and the resource
// endpoints disagree about how a failure looks.
func (c *client) do(req *http.Request) (result, error) {
	started := time.Now()

	resp, err := c.hc.Do(req)
	if err != nil {
		return result{}, fmt.Errorf("%s %s: %w", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return result{}, fmt.Errorf("reading %s %s: %w", req.Method, req.URL, err)
	}

	return result{
		Status:   resp.StatusCode,
		Body:     body,
		Duration: time.Since(started),
	}, nil
}

func prettyJSON(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return string(b)
	}
	return out.String()
}
