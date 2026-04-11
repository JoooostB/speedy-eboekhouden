package mail

import (
	"fmt"
	"html"
	"net/smtp"
	"strings"
)

// Config holds SMTP connection settings.
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

// Mailer sends transactional emails via SMTP.
type Mailer struct {
	config Config
}

// New creates a new mailer. Returns nil if SMTP is not configured.
func New(cfg Config) *Mailer {
	if cfg.Host == "" {
		return nil
	}
	return &Mailer{config: cfg}
}

// sanitizeHeader strips CR/LF to prevent email header injection.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// SendRecoveryEmail sends a passkey recovery link.
func (m *Mailer) SendRecoveryEmail(to, name, recoveryURL string) error {
	subject := "Nieuwe passkey instellen — Speedy e-Boekhouden"
	plain := fmt.Sprintf("Hoi %s,\r\n\r\n"+
		"Je hebt een nieuwe passkey aangevraagd voor je Speedy e-Boekhouden account.\r\n\r\n"+
		"Klik op de volgende link om een nieuwe passkey in te stellen:\r\n%s\r\n\r\n"+
		"Deze link is 15 minuten geldig en kan maar een keer worden gebruikt.\r\n\r\n"+
		"Heb je dit niet aangevraagd? Dan kun je deze e-mail negeren.\r\n\r\n"+
		"Met vriendelijke groet,\r\nSpeedy e-Boekhouden",
		sanitizeHeader(name), sanitizeHeader(recoveryURL))
	return m.sendPlain(sanitizeHeader(to), subject, plain)
}

// SendWelcomeEmail sends a branded welcome email after registration.
func (m *Mailer) SendWelcomeEmail(to, name, appURL string) error {
	subject := "Welkom bij Speedy e-Boekhouden"
	// HTML-escape user-supplied values to prevent HTML injection
	safeName := html.EscapeString(name)
	safeURL := html.EscapeString(appURL)
	htmlBody := welcomeHTML(safeName, safeURL)
	plain := fmt.Sprintf("Hoi %s,\r\n\r\n"+
		"Welkom bij Speedy e-Boekhouden! Je account is aangemaakt.\r\n\r\n"+
		"Wat kun je doen?\r\n"+
		"- Uren in bulk invoeren voor je hele team\r\n"+
		"- Bankafschriften verwerken met AI-suggesties\r\n"+
		"- Inkoopfacturen automatisch laten uitlezen\r\n\r\n"+
		"Ga aan de slag: %s\r\n\r\n"+
		"Met vriendelijke groet,\r\nSpeedy e-Boekhouden",
		sanitizeHeader(name), sanitizeHeader(appURL))
	return m.sendHTML(sanitizeHeader(to), subject, htmlBody, plain)
}

func (m *Mailer) sendPlain(to, subject, body string) error {
	msg := "From: Speedy e-Boekhouden <" + sanitizeHeader(m.config.From) + ">\r\n" +
		"To: " + sanitizeHeader(to) + "\r\n" +
		"Subject: " + sanitizeHeader(subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body

	return m.deliver(to, []byte(msg))
}

func (m *Mailer) sendHTML(to, subject, htmlBody, plain string) error {
	boundary := "----=_SpeedyBoundary"
	msg := "From: Speedy e-Boekhouden <" + sanitizeHeader(m.config.From) + ">\r\n" +
		"To: " + sanitizeHeader(to) + "\r\n" +
		"Subject: " + sanitizeHeader(subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + plain + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"\r\n" + htmlBody + "\r\n" +
		"--" + boundary + "--\r\n"

	return m.deliver(to, []byte(msg))
}

func (m *Mailer) deliver(to string, msg []byte) error {
	auth := smtp.PlainAuth("", m.config.Username, m.config.Password, m.config.Host)
	addr := m.config.Host + ":" + m.config.Port
	return smtp.SendMail(addr, auth, m.config.From, []string{to}, msg)
}
