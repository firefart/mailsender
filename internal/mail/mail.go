package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	gomail "github.com/wneessen/go-mail"
)

type Mail struct {
	client *gomail.Client
	dryRun bool
}

func New(host string, port int, username, password string, useTLS, useStartTLS, skipCertificateCheck bool, timeout time.Duration, dryRun bool) (*Mail, error) {
	var options []gomail.Option

	options = append(options, gomail.WithTimeout(timeout))
	options = append(options, gomail.WithPort(port))
	if username != "" && password != "" {
		options = append(options, gomail.WithSMTPAuth(gomail.SMTPAuthPlain))
		options = append(options, gomail.WithUsername(username))
		options = append(options, gomail.WithPassword(password))
	}
	if skipCertificateCheck {
		options = append(options, gomail.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		}))
	}

	// use either tls, starttls, or starttls with fallback to plaintext
	if useTLS {
		options = append(options, gomail.WithSSL())
	} else if useStartTLS {
		options = append(options, gomail.WithTLSPortPolicy(gomail.TLSMandatory))
	} else {
		options = append(options, gomail.WithTLSPortPolicy(gomail.TLSOpportunistic))
	}

	mailer, err := gomail.NewClient(host, options...)
	if err != nil {
		return nil, fmt.Errorf("could not create mail client: %w", err)
	}

	return &Mail{
		client: mailer,
		dryRun: dryRun,
	}, nil
}

func (m *Mail) Send(ctx context.Context, fromFriendly, fromEmail, to, subject, bodyHTML, bodyTXT string) error {
	if m.dryRun {
		// do nothing in dry-run mode
		return nil
	}

	msg := gomail.NewMsg(gomail.WithNoDefaultUserAgent())
	if err := msg.FromFormat(fromFriendly, fromEmail); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, bodyTXT)
	msg.AddAlternativeString(gomail.TypeTextHTML, bodyHTML)

	if err := m.client.DialAndSendWithContext(ctx, msg); err != nil {
		return err
	}
	return nil
}
