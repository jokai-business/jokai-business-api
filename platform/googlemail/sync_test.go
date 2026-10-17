package googlemail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMessageParsing(t *testing.T) {
	raw := rawMessage{ID: "abc123", Date: "1792022400000", Labels: []string{"UNREAD"}, Snippet: "Hello &amp; world"}
	raw.Payload.MimeType = "multipart/mixed"
	raw.Payload.Headers = append(raw.Payload.Headers, struct{ Name, Value string }{"From", "Alice <ALICE@example.test>"}, struct{ Name, Value string }{"Subject", "=?UTF-8?B?SGVsbG8=?="})
	text := part{MimeType: "text/plain"}
	text.Body.Data = base64.RawURLEncoding.EncodeToString([]byte("Plain text\x00"))
	html := part{MimeType: "text/html"}
	html.Body.Data = base64.RawURLEncoding.EncodeToString([]byte("<img src='https://tracking.test'>"))
	attachment := text
	attachment.Filename = "private.txt"
	attachment.Body.Data = base64.RawURLEncoding.EncodeToString([]byte("attachment-secret"))
	raw.Payload.Parts = []part{text, html, attachment}
	m, e := parseMessage(raw)
	if e != nil || m.Text != "Plain text" || m.SenderEmail != "alice@example.test" || m.Subject != "Hello" || !m.Unread || m.Snippet != "Hello & world" {
		t.Fatal(m, e)
	}
	raw.Payload.Headers = append(raw.Payload.Headers, struct{ Name, Value string }{"From", "Other <other@example.test>"})
	m, _ = parseMessage(raw)
	if m.SenderEmail != "" {
		t.Fatal("ambiguous sender matched")
	}
	raw.Payload.Parts = []part{html}
	m, _ = parseMessage(raw)
	if m.Text != "" || !m.TextLimited {
		t.Fatal("HTML rendered as body")
	}
	text.Body.Data = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 33000)))
	raw.Payload.Parts = []part{text}
	m, _ = parseMessage(raw)
	if len(m.Text) != 32000 || !m.TextLimited {
		t.Fatal("body unbounded")
	}
	raw.ID = "../bad"
	if _, e = parseMessage(raw); e == nil {
		t.Fatal("unsafe ID")
	}
}
func TestRecentProtocol(t *testing.T) {
	since := time.Now().Add(-30 * 24 * time.Hour)
	mode := ""
	details := 0
	var listCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" {
				t.Error("refresh protocol")
			}
			fmt.Fprint(w, `{"access_token":"access","token_type":"Bearer"}`)
		case "/profile":
			email := "owner@example.test"
			if mode == "wrong-mailbox" {
				email = "another@example.test"
			}
			fmt.Fprintf(w, `{"emailAddress":%q}`, email)
		case "/messages":
			listCalls++
			if r.Header.Get("Authorization") != "Bearer access" || r.URL.Query().Get("maxResults") == "" || !strings.Contains(r.URL.Query().Get("q"), "after:") {
				t.Error("unbounded list")
			}
			if mode == "repeated-page" {
				fmt.Fprint(w, `{"messages":[],"nextPageToken":"same"}`)
				return
			}
			fmt.Fprint(w, `{"messages":[{"id":"one"},{"id":"gone"}]}`)
		case "/messages/gone":
			http.NotFound(w, r)
		case "/messages/one":
			details++
			if mode == "failure" {
				http.Error(w, "private-detail", 500)
				return
			}
			fmt.Fprintf(w, `{"id":"one","internalDate":%q,"snippet":"preview","labelIds":["UNREAD"],"payload":{"mimeType":"text/plain","headers":[{"name":"From","value":"Sender <sender@example.test>"}],"body":{"data":"SGVsbG8"}}}`, fmt.Sprint(time.Now().UnixMilli()))
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer server.Close()
	c := New(Config{ClientID: "client", ClientSecret: "secret"})
	c.tokenURL = server.URL + "/token"
	c.profileURL = server.URL + "/profile"
	result, e := c.Recent(context.Background(), "refresh", "owner@example.test", since)
	if e != nil || len(result.Messages) != 1 || result.Messages[0].Text != "Hello" || details != 1 {
		t.Fatal(result, e)
	}
	mode = "wrong-mailbox"
	if _, e = c.Recent(context.Background(), "refresh", "owner@example.test", since); e != ErrProvider {
		t.Fatal("mailbox mismatch", e)
	}
	mode = "failure"
	if _, e = c.Recent(context.Background(), "refresh", "owner@example.test", since); e != ErrProvider {
		t.Fatal("partial fetch accepted", e)
	}
	mode = "repeated-page"
	listCalls = 0
	if _, e = c.Recent(context.Background(), "refresh", "owner@example.test", since); e != ErrProvider || listCalls != 2 {
		t.Fatal("unbounded pagination", listCalls, e)
	}
}

func TestRecentCap(t *testing.T) {
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"access","token_type":"Bearer"}`)
		case "/profile":
			fmt.Fprint(w, `{"emailAddress":"owner@example.test"}`)
		case "/messages":
			listCalls++
			if r.URL.Query().Get("maxResults") != "500" {
				t.Error("incorrect cap")
			}
			fmt.Fprint(w, `{"messages":[`)
			for i := 0; i < 500; i++ {
				if i > 0 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, `{"id":"msg%d"}`, i)
			}
			fmt.Fprint(w, `],"nextPageToken":"more"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := New(Config{})
	c.tokenURL = server.URL + "/token"
	c.profileURL = server.URL + "/profile"
	result, e := c.Recent(context.Background(), "refresh", "owner@example.test", time.Now().Add(-30*24*time.Hour))
	if e != nil || !result.Limited || listCalls != 1 {
		t.Fatal("cap not enforced", result.Limited, listCalls, e)
	}
}
