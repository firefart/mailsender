package mail // revive:disable:var-naming

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/firefart/mailsender/internal/config"

	gomail "github.com/wneessen/go-mail"
)

type Mail struct {
	client *gomail.Client
	dryRun bool
}

func New(c config.SystemConfiguration, dryRun bool) (*Mail, error) {
	var options []gomail.Option

	options = append(options, gomail.WithTimeout(c.Timeout.Duration))
	options = append(options, gomail.WithPort(c.Port))
	if c.User != "" && c.Password != "" {
		options = append(options, gomail.WithSMTPAuth(gomail.SMTPAuthPlain))
		options = append(options, gomail.WithUsername(c.User))
		options = append(options, gomail.WithPassword(c.Password))
	}
	if c.SkipCertificateCheck {
		options = append(options, gomail.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		}))
	}

	// use either tls, starttls, or starttls with fallback to plaintext
	if c.TLS {
		options = append(options, gomail.WithSSL())
	} else if c.StartTLS {
		options = append(options, gomail.WithTLSPortPolicy(gomail.TLSMandatory))
	} else {
		options = append(options, gomail.WithTLSPortPolicy(gomail.TLSOpportunistic))
	}

	mailer, err := gomail.NewClient(c.Server, options...)
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
