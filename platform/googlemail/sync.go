package googlemail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const MaxMessages = 500

var messageID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type Message struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	Sender      string    `json:"sender"`
	SenderEmail string    `json:"senderEmail"`
	Snippet     string    `json:"snippet"`
	Text        string    `json:"text"`
	Unread      bool      `json:"unread"`
	ReceivedAt  time.Time `json:"receivedAt"`
	TextLimited bool      `json:"textLimited"`
}
type Snapshot struct {
	Messages []Message
	Limited  bool
}
type part struct {
	MimeType string                         `json:"mimeType"`
	Filename string                         `json:"filename"`
	Headers  []struct{ Name, Value string } `json:"headers"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []part `json:"parts"`
}
type rawMessage struct {
	ID      string   `json:"id"`
	Date    string   `json:"internalDate"`
	Labels  []string `json:"labelIds"`
	Snippet string   `json:"snippet"`
	Payload part     `json:"payload"`
}

func clean(v string, n int) string {
	v = strings.ReplaceAll(strings.ToValidUTF8(v, "�"), "\x00", "")
	if len(v) <= n {
		return v
	}
	v = v[:n]
	for !utf8.ValidString(v) {
		v = v[:len(v)-1]
	}
	return v
}
func parseMessage(raw rawMessage) (Message, error) {
	ms, e := strconv.ParseInt(raw.Date, 10, 64)
	if e != nil || ms <= 0 || !messageID.MatchString(raw.ID) {
		return Message{}, ErrProvider
	}
	m := Message{ID: raw.ID, ReceivedAt: time.UnixMilli(ms).UTC(), Snippet: clean(html.UnescapeString(raw.Snippet), 2000)}
	fromCount := 0
	for _, h := range raw.Payload.Headers {
		switch strings.ToLower(h.Name) {
		case "subject":
			m.Subject = clean(h.Value, 1000)
		case "from":
			fromCount++
			m.Sender = clean(h.Value, 1000)
		}
	}
	if decoded, e := new(mime.WordDecoder).DecodeHeader(m.Subject); e == nil {
		m.Subject = clean(decoded, 1000)
	}
	if fromCount == 1 {
		if addr, e := mail.ParseAddress(m.Sender); e == nil && len(addr.Address) <= 254 {
			m.SenderEmail = strings.ToLower(addr.Address)
		}
	}
	for _, label := range raw.Labels {
		if label == "UNREAD" {
			m.Unread = true
		}
	}
	nodes := 0
	var visit func(part, int)
	visit = func(p part, depth int) {
		nodes++
		if nodes > 100 || depth > 10 {
			m.TextLimited = true
			return
		}
		if p.Filename != "" {
			return
		}
		if strings.EqualFold(p.MimeType, "text/plain") && p.Body.Data != "" {
			data, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "="))
			if e != nil {
				m.TextLimited = true
				return
			}
			if !utf8.Valid(data) {
				m.TextLimited = true
				return
			}
			text := string(data)
			if m.Text != "" {
				text = "\n" + text
			}
			if len(m.Text)+len(text) > 32000 {
				m.TextLimited = true
			}
			m.Text = clean(m.Text+text, 32000)
		}
		if strings.HasPrefix(strings.ToLower(p.MimeType), "multipart/") {
			for _, child := range p.Parts {
				visit(child, depth+1)
			}
		}
	}
	visit(raw.Payload, 0)
	if m.Text == "" {
		m.TextLimited = true
	}
	return m, nil
}

// fetchMessage never follows embedded URLs or calls the attachment endpoint. HTML is not returned to the app.
func (c *Client) fetchMessage(ctx context.Context, endpoint, token string) (rawMessage, bool, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return rawMessage{}, false, ErrProvider
	}
	r.Header.Set("Authorization", "Bearer "+token)
	response, e := c.http.Do(r)
	if e != nil {
		return rawMessage{}, false, ErrProvider
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return rawMessage{}, true, nil
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if e != nil || len(data) > 2*1024*1024 || response.StatusCode != 200 {
		return rawMessage{}, false, ErrProvider
	}
	var raw rawMessage
	if json.Unmarshal(data, &raw) != nil {
		return rawMessage{}, false, ErrProvider
	}
	return raw, false, nil
}
func (c *Client) Recent(ctx context.Context, refresh, email string, since time.Time) (Snapshot, error) {
	var tok struct {
		Access string `json:"access_token"`
		Type   string `json:"token_type"`
	}
	if e := c.request(ctx, "POST", c.tokenURL, url.Values{"client_id": {c.config.ClientID}, "client_secret": {c.config.ClientSecret}, "refresh_token": {refresh}, "grant_type": {"refresh_token"}}, "", &tok); e != nil {
		return Snapshot{}, e
	}
	if tok.Access == "" || !strings.EqualFold(tok.Type, "Bearer") || strings.ContainsAny(tok.Access, "\r\n") {
		return Snapshot{}, ErrProvider
	}
	var profile struct {
		Email string `json:"emailAddress"`
	}
	if e := c.request(ctx, "GET", c.profileURL, nil, tok.Access, &profile); e != nil || !strings.EqualFold(profile.Email, email) {
		return Snapshot{}, ErrProvider
	}
	base := strings.TrimSuffix(c.profileURL, "/profile") + "/messages"
	ids := []string{}
	seen := map[string]bool{}
	pages := map[string]bool{}
	next := ""
	limited := false
	for page := 0; page < 10; page++ {
		q := url.Values{"maxResults": {strconv.Itoa(MaxMessages - len(ids))}, "q": {"after:" + strconv.FormatInt(since.Unix(), 10) + " -in:spam -in:trash -in:drafts"}, "includeSpamTrash": {"false"}, "fields": {"messages(id),nextPageToken"}}
		if next != "" {
			q.Set("pageToken", next)
		}
		var list struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			Next string `json:"nextPageToken"`
		}
		if e := c.request(ctx, "GET", base+"?"+q.Encode(), nil, tok.Access, &list); e != nil {
			return Snapshot{}, e
		}
		if len(list.Messages) > MaxMessages-len(ids) {
			return Snapshot{}, ErrProvider
		}
		for _, item := range list.Messages {
			if !messageID.MatchString(item.ID) {
				return Snapshot{}, ErrProvider
			}
			if !seen[item.ID] {
				seen[item.ID] = true
				ids = append(ids, item.ID)
			}
		}
		next = list.Next
		if next == "" {
			break
		}
		if len(next) > 4096 || pages[next] {
			return Snapshot{}, ErrProvider
		}
		pages[next] = true
		if len(ids) == MaxMessages || page == 9 {
			limited = true
			break
		}
	}
	result := Snapshot{Messages: []Message{}, Limited: limited}
	var mu sync.Mutex
	var first error
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for id := range jobs {
				raw, missing, e := c.fetchMessage(ctx, base+"/"+url.PathEscape(id)+"?format=full", tok.Access)
				if e == nil && !missing && raw.ID != id {
					e = ErrProvider
				}
				for _, label := range raw.Labels {
					if label == "TRASH" || label == "SPAM" || label == "DRAFT" {
						missing = true
					}
				}
				var m Message
				if e == nil && !missing {
					m, e = parseMessage(raw)
				}
				mu.Lock()
				if e != nil {
					first = e
				} else if !missing && !m.ReceivedAt.Before(since) && !m.ReceivedAt.After(time.Now().Add(24*time.Hour)) {
					result.Messages = append(result.Messages, m)
				}
				mu.Unlock()
			}
		})
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return Snapshot{}, ErrProvider
		}
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return Snapshot{}, first
	}
	return result, nil
}
