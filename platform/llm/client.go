// Package llm provides one operator-configured Chat Completions protocol adapter.
package llm

import (
	"ai-business-api/internal/ai"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrProvider = errors.New("AI provider request failed")

type Config struct{ URL, Key, Model, Label string }

func Read(get func(string) string) (Config, error) {
	c := Config{get("LLM_API_URL"), get("LLM_API_KEY"), get("LLM_MODEL"), get("LLM_PROVIDER_LABEL")}
	if c == (Config{}) {
		return c, nil
	}
	u, e := url.Parse(c.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || !strings.HasSuffix(u.Path, "/chat/completions") || len(c.Key) < 1 || len(c.Key) > 4096 || len(c.Model) < 1 || len(c.Model) > 128 || len(c.Label) < 1 || len(c.Label) > 80 || strings.ContainsAny(c.Key+c.Model+c.Label, "\r\n\x00") {
		return Config{}, errors.New("configure LLM_API_URL (HTTPS chat/completions endpoint), LLM_API_KEY, LLM_MODEL and LLM_PROVIDER_LABEL together")
	}
	return c, nil
}
func (c Config) Enabled() bool { return c.URL != "" }

type Client struct {
	config Config
	http   *http.Client
}

func New(c Config) *Client {
	return &Client{c, &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Generate(ctx context.Context, r ai.Request) (string, error) {
	if len(r.Input) > 40000 || len(r.System) > 8000 {
		return "", ErrProvider
	}
	payload := struct {
		Model    string              `json:"model"`
		Messages []map[string]string `json:"messages"`
		Max      int                 `json:"max_completion_tokens"`
		Store    bool                `json:"store"`
	}{c.config.Model, []map[string]string{{"role": "system", "content": r.System}, {"role": "user", "content": r.Input}}, 2048, false}
	b, e := json.Marshal(payload)
	if e != nil {
		return "", ErrProvider
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, bytes.NewReader(b))
	if e != nil {
		return "", ErrProvider
	}
	req.Header.Set("Authorization", "Bearer "+c.config.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return "", ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", ErrProvider
	}
	b, e = io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(b) > 65536 {
		return "", ErrProvider
	}
	var out struct {
		Choices []struct {
			Finish  string `json:"finish_reason"`
			Message struct {
				Content string          `json:"content"`
				Refusal string          `json:"refusal"`
				Tools   json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(b, &out) != nil || len(out.Choices) != 1 {
		return "", ErrProvider
	}
	choice := out.Choices[0]
	text := strings.TrimSpace(choice.Message.Content)
	if choice.Finish != "stop" || choice.Message.Refusal != "" || (len(choice.Message.Tools) > 0 && string(choice.Message.Tools) != "null" && string(choice.Message.Tools) != "[]") || !ai.ValidText(text) {
		return "", ErrProvider
	}
	return text, nil
}
