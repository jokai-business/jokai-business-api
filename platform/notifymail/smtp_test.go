package notifymail

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	env := map[string]string{"NOTIFICATION_EMAIL_ENABLED": "true", "NOTIFICATION_SMTP_HOST": "smtp.example.test", "NOTIFICATION_SMTP_USERNAME": "user", "NOTIFICATION_SMTP_PASSWORD": "secret", "NOTIFICATION_SMTP_FROM": "notify@example.test", "APP_URL": "https://app.example.test"}
	get := func(k string) string { return env[k] }
	c, e := Read(get)
	if e != nil || c.Port != "587" || !c.Enabled {
		t.Fatalf("config: %+v %v", c, e)
	}
	for _, tc := range []struct{ k, v string }{{"NOTIFICATION_EMAIL_ENABLED", "yes"}, {"NOTIFICATION_SMTP_HOST", "bad\r\nhost"}, {"NOTIFICATION_SMTP_PORT", "65536"}, {"NOTIFICATION_SMTP_PASSWORD", ""}, {"NOTIFICATION_SMTP_FROM", "a@example.test\r\nBcc: b@example.test"}, {"APP_URL", "http://app.example.test"}, {"APP_URL", "https://user:pass@app.example.test"}, {"APP_URL", "https://app.example.test/path"}} {
		old := env[tc.k]
		env[tc.k] = tc.v
		if _, e = Read(get); e == nil {
			t.Errorf("accepted %s=%s", tc.k, tc.v)
		}
		env[tc.k] = old
	}
	c, e = Read(func(string) string { return "" })
	if e != nil || c.Enabled {
		t.Fatal("must default disabled")
	}
}
func TestMessageInjection(t *testing.T) {
	c := Config{From: "notify@example.test", AppURL: "https://app.example.test"}
	for _, to := range []string{"a@example.test\r\nBcc: b@example.test", "Name <a@example.test>", ""} {
		if _, e := message(c, to, "id"); e == nil {
			t.Fatal("accepted invalid recipient")
		}
	}
	if _, e := message(c, "a@example.test", "id\r\nSubject: bad"); e == nil {
		t.Fatal("accepted invalid id")
	}
	msg, e := message(c, "a@example.test", "test-id")
	if e != nil || !strings.Contains(string(msg), "https://app.example.test/notifications\r\n") {
		t.Fatal("invalid message")
	}
}
func TestRefusesPlaintextServer(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		conn.Write([]byte("220 localhost ESMTP\r\n"))
		buf := make([]byte, 512)
		conn.Read(buf)
		conn.Write([]byte("250 localhost\r\n"))
		conn.Read(buf)
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	s := Sender{Config{Enabled: true, Host: host, Port: port, From: "notify@example.test", AppURL: "https://app.example.test", Username: "u", Password: "p"}}
	if e := s.Send(context.Background(), "a@example.test", "test-id"); e == nil || !strings.Contains(e.Error(), "STARTTLS") {
		t.Fatalf("wanted TLS refusal: %v", e)
	}
	<-done
}

func TestCancelledSMTPConnection(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	accepted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 1)
		conn.Read(buf)
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	s := Sender{Config{Enabled: true, Host: host, Port: port, From: "notify@example.test", AppURL: "https://app.example.test"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Send(ctx, "a@example.test", "test-id") }()
	<-accepted
	cancel()
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("cancelled send succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP did not stop on cancellation")
	}
	<-done
}
