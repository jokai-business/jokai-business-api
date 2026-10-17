// Package paddleocr implements the PaddleOCR 3.x basic serving /ocr protocol.
package paddleocr

import (
	"ai-business-api/internal/document"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func New(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("OCR_URL must be an HTTP(S) /ocr endpoint without credentials, query or fragment")
	}
	return &Client{endpoint: endpoint, http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Extract(ctx context.Context, data []byte, contentType string) (document.OCRResult, error) {
	fail := errors.New("PaddleOCR extraction failed")
	typ := 1
	if contentType == "application/pdf" {
		typ = 0
	} else if contentType != "image/png" && contentType != "image/jpeg" {
		return document.OCRResult{}, fail
	}
	payload, _ := json.Marshal(map[string]any{"file": base64.StdEncoding.EncodeToString(data), "fileType": typ, "useDocOrientationClassify": false, "useDocUnwarping": false, "useTextlineOrientation": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return document.OCRResult{}, fail
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return document.OCRResult{}, fail
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return document.OCRResult{}, fail
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(body) > 16<<20 {
		return document.OCRResult{}, fail
	}
	var v struct {
		ErrorCode *int `json:"errorCode"`
		Result    struct {
			OCRResults []struct {
				PrunedResult struct {
					Texts []string `json:"rec_texts"`
				} `json:"prunedResult"`
			} `json:"ocrResults"`
			DataInfo struct {
				NumPages int `json:"numPages"`
			} `json:"dataInfo"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &v) != nil || v.ErrorCode == nil || *v.ErrorCode != 0 || len(v.Result.OCRResults) < 1 || len(v.Result.OCRResults) > 10 || v.Result.DataInfo.NumPages > len(v.Result.OCRResults) {
		return document.OCRResult{}, fail
	}
	pages := make([]string, 0, len(v.Result.OCRResults))
	hasText := false
	for _, p := range v.Result.OCRResults {
		for _, line := range p.PrunedResult.Texts {
			if strings.TrimSpace(line) != "" {
				hasText = true
			}
		}
		pages = append(pages, strings.Join(p.PrunedResult.Texts, "\n"))
	}
	result := document.OCRResult{Text: strings.Join(pages, "\n\n--- Page break ---\n\n"), Pages: len(pages)}
	if !hasText || len(result.Text) > document.MaxOCRText || strings.TrimSpace(result.Text) == "" {
		return document.OCRResult{}, fail
	}
	return result, nil
}

// CheckHealth uses the service's health endpoint without submitting a document.
func (c *Client) CheckHealth(ctx context.Context) error {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return errors.New("OCR health unavailable")
	}
	u.Path = "/health"
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("OCR health unavailable")
	}
	var body struct {
		Status string `json:"status"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	if err != nil || len(data) > 1024 || json.Unmarshal(data, &body) != nil || body.Status != "ready" {
		return errors.New("OCR health unavailable")
	}
	return nil
}
