// Package notifymail sends generic notification emails through authenticated STARTTLS.
package notifymail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Enabled                                      bool
	Host, Port, Username, Password, From, AppURL string
}

func Read(get func(string) string) (Config, error) {
	c := Config{Host: get("NOTIFICATION_SMTP_HOST"), Port: get("NOTIFICATION_SMTP_PORT"), Username: get("NOTIFICATION_SMTP_USERNAME"), Password: get("NOTIFICATION_SMTP_PASSWORD"), From: get("NOTIFICATION_SMTP_FROM"), AppURL: get("APP_URL")}
	enabled := get("NOTIFICATION_EMAIL_ENABLED")
	if enabled != "" && enabled != "false" && enabled != "true" {
		return c, errors.New("NOTIFICATION_EMAIL_ENABLED must be true or false")
	}
	c.Enabled = enabled == "true"
	if !c.Enabled {
		return c, nil
	}
	if c.Port == "" {
		c.Port = "587"
	}
	port, e := strconv.Atoi(c.Port)
	u, ue := url.Parse(c.AppURL)
	if c.Host == "" || strings.ContainsAny(c.Host, "\r\n /:") || port < 1 || port > 65535 || e != nil || c.Username == "" || c.Password == "" || !address(c.From) || ue != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, errors.New("invalid notification SMTP configuration: require host, port, credentials, sender and HTTPS APP_URL origin")
	}
	c.AppURL = strings.TrimRight(c.AppURL, "/")
	return c, nil
}
func address(s string) bool {
	a, e := mail.ParseAddress(s)
	return e == nil && a.Address == s && !strings.ContainsAny(s, "\r\n")
}
func message(c Config, to, id string) ([]byte, error) {
	if !address(to) || !address(c.From) || id == "" || strings.ContainsAny(id, "\r\n<>@ ") {
		return nil, errors.New("invalid notification email envelope")
	}
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: JOKAI notification\r\nMessage-ID: <%s@jokai.notifications>\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYou have a new notification in JOKAI. Sign in to review it:\r\n%s/notifications\r\n\r\nManage email notifications from your Notifications page.\r\n", c.From, to, id, time.Now().UTC().Format(time.RFC1123Z), c.AppURL)), nil
}

type Sender struct{ Config Config }

func (s Sender) Send(ctx context.Context, to, id string) error     { return s.send(ctx, to, id, false) }
func (s Sender) SendTest(ctx context.Context, to, id string) error { return s.send(ctx, to, id, true) }
func (s Sender) send(ctx context.Context, to, id string, test bool) error {
	c := s.Config
	if !c.Enabled {
		return errors.New("notification email disabled")
	}
	msg, e := message(c, to, id)
	if e != nil {
		return e
	}
	if test {
		msg = []byte(strings.Replace(strings.Replace(string(msg), "Subject: JOKAI notification", "Subject: JOKAI email delivery test", 1), "You have a new notification in JOKAI. Sign in to review it:", "This test email was requested from JOKAI Admin Settings. No business records were changed. Open your workspace:", 1))
	}
	conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, c.Port))
	if e != nil {
		return e
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e = conn.SetDeadline(deadline); e != nil {
		return e
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	client, e := smtp.NewClient(conn, c.Host)
	if e != nil {
		return e
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("SMTP server requires STARTTLS support")
	}
	if e = client.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); e != nil {
		return e
	}
	if e = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); e != nil {
		return e
	}
	if e = client.Mail(c.From); e != nil {
		return e
	}
	if e = client.Rcpt(to); e != nil {
		return e
	}
	w, e := client.Data()
	if e != nil {
		return e
	}
	if _, e = w.Write(msg); e != nil {
		return e
	}
	if e = w.Close(); e != nil {
		return e
	}
	// DATA acceptance is the delivery boundary; a failed QUIT must not trigger a retry.
	_ = client.Quit()
	return nil
}
