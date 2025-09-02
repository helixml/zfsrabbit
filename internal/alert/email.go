package alert

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"zfsrabbit/internal/config"
)

type EmailAlerter struct {
	config *config.EmailConfig
}

func NewEmailAlerter(cfg *config.EmailConfig) *EmailAlerter {
	return &EmailAlerter{
		config: cfg,
	}
}

func (e *EmailAlerter) SendAlert(subject, body string) error {
	if e.config.SMTPHost == "" || len(e.config.ToEmails) == 0 {
		return fmt.Errorf("email configuration incomplete")
	}

	auth := smtp.PlainAuth("", e.config.SMTPUser, e.config.SMTPPassword, e.config.SMTPHost)

	msg := e.buildMessage(subject, body)

	addr := fmt.Sprintf("%s:%d", e.config.SMTPHost, e.config.SMTPPort)

	if e.config.UseTLS {
		return e.sendTLS(addr, auth, msg)
	}

	return smtp.SendMail(addr, auth, e.config.FromEmail, e.config.ToEmails, []byte(msg))
}

func (e *EmailAlerter) sendTLS(addr string, auth smtp.Auth, msg string) error {
	tlsConfig := &tls.Config{
		ServerName: e.config.SMTPHost,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, e.config.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()

	if err = client.Auth(auth); err != nil {
		return err
	}

	if err = client.Mail(e.config.FromEmail); err != nil {
		return err
	}

	for _, to := range e.config.ToEmails {
		if err = client.Rcpt(to); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}

	_, err = writer.Write([]byte(msg))
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		return err
	}

	return client.Quit()
}

func (e *EmailAlerter) buildMessage(subject, body string) string {
	headers := make(map[string]string)
	headers["From"] = e.config.FromEmail
	headers["To"] = strings.Join(e.config.ToEmails, ";")
	headers["Subject"] = fmt.Sprintf("[ZFSRabbit] %s", subject)
	headers["Date"] = time.Now().Format(time.RFC822)
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=utf-8"

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	return message
}

func (e *EmailAlerter) TestConnection() error {
	return e.SendAlert("Test Alert", "This is a test email from ZFSRabbit to verify email configuration.")
}
