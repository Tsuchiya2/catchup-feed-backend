package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig points at an existing free SMTP relay (C-11 / ゼロ円運用:
// e.g. Gmail SMTP with an app password — no new SaaS). Port 465 selects
// implicit TLS; anything else dials plain and upgrades via STARTTLS when
// the server offers it.
type SMTPConfig struct {
	Host     string        // SMTP_HOST, e.g. smtp.gmail.com
	Port     int           // SMTP_PORT, e.g. 587
	Username string        // SMTP_USERNAME; empty = no AUTH (local relay)
	Password string        // SMTP_PASSWORD, e.g. a Gmail app password
	From     string        // SMTP_FROM sender address
	Timeout  time.Duration // whole-session ceiling
}

// SMTPMailer sends plain-text mail over SMTP — the friend notification
// channel (C-11: 友人=メール). Deliberately naive: one connection per
// message, text/plain UTF-8, no HTML, no queueing. Friends number in the
// single digits and retries are the jobs queue's concern (§7).
type SMTPMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer builds a mailer for the given relay.
func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &SMTPMailer{cfg: cfg}
}

// Send delivers one message to one recipient. Friends are addressed
// individually (never as a shared To list) so addresses are not leaked
// between them.
func (m *SMTPMailer) Send(ctx context.Context, to, subject, body string) error {
	msg, err := buildMail(m.cfg.From, to, subject, body, time.Now())
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	dialer := &net.Dialer{Timeout: m.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: dial %s: %w", addr, err)
	}
	// One deadline for the whole SMTP conversation: the earlier of the
	// context deadline and the configured timeout.
	deadline := time.Now().Add(m.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: set deadline: %w", err)
	}

	if m.cfg.Port == 465 { // implicit TLS
		conn = tls.Client(conn, &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12})
	}
	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if m.cfg.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
			if err := client.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("smtp: starttls: %w", err)
			}
		}
	}
	if m.cfg.Username != "" {
		// PlainAuth refuses to send credentials over unencrypted
		// connections to non-localhost servers — the safety net if the
		// relay unexpectedly lacks STARTTLS.
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: finish body: %w", err)
	}
	return client.Quit()
}

// buildMail renders an RFC 5322 message: text/plain UTF-8 with a
// base64-encoded body and a MIME-encoded subject, so Japanese survives
// every relay. Addresses are rejected when they could smuggle extra
// headers (CR/LF injection); the subject needs no check because B-encoding
// cannot emit raw newlines.
func buildMail(from, to, subject, body string, now time.Time) ([]byte, error) {
	for _, addr := range []string{from, to} {
		if addr == "" || strings.ContainsAny(addr, "\r\n") {
			return nil, errors.New("smtp: invalid address")
		}
	}
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(wrapBase64(base64.StdEncoding.EncodeToString([]byte(body))))
	return []byte(b.String()), nil
}

// wrapBase64 folds the base64 body at 76 columns (RFC 2045).
func wrapBase64(s string) string {
	const width = 76
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteString("\r\n")
		s = s[width:]
	}
	b.WriteString(s)
	b.WriteString("\r\n")
	return b.String()
}
