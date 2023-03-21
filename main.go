package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"text/template"

	"github.com/firefart/mailsender/internal/config"
	"github.com/firefart/mailsender/internal/mail"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

type importOptions struct {
	dbname      string
	csvFilePath string
}

func (o importOptions) validate() error {
	if o.dbname == "" {
		return fmt.Errorf("please set a database name")
	}

	if o.csvFilePath == "" {
		return fmt.Errorf("please set a csv file name")
	}

	return nil
}

type sendOptions struct {
	dbname         string
	numberOfEmails int
	stopOnError    bool
	config         config.Configuration
}

func (o sendOptions) validate() error {
	if o.dbname == "" {
		return fmt.Errorf("please set a database name")
	}

	if o.config.Templates.HTML == "" {
		return fmt.Errorf("please set a html template path")
	}

	if o.config.Templates.TXT == "" {
		return fmt.Errorf("please set a txt template path")
	}

	if o.config.Mail.From.Name == "" {
		return fmt.Errorf("please set a friendly from name")
	}

	if o.config.Mail.From.Mail == "" {
		return fmt.Errorf("please set a from email")
	}

	if o.config.Subject == "" {
		return fmt.Errorf("please set a subject")
	}

	if o.config.Mail.Server == "" {
		return fmt.Errorf("please set a mail host")
	}

	if o.config.Mail.Port == 0 {
		return fmt.Errorf("please set a mail port")
	}
	return nil
}

type candidate struct {
	id        int
	name      string
	givenName string
	email     string
}

type templateData struct {
	Name      string
	GivenName string
	Email     string
}

func main() {
	log := logrus.New()
	app := &cli.App{
		Name:  "mailsender",
		Usage: "sends a bunch of emails and tracks the status",
		Commands: []*cli.Command{
			{
				Name:  "import",
				Usage: "import list of emails to the database",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "debug", Aliases: []string{"d"}, Value: false, Usage: "enable debug output"},
					&cli.StringFlag{Name: "dbname", Usage: "local database name", Value: "emails.db"},
					&cli.StringFlag{Name: "csv", Required: true, Usage: "csv file to import"},
				},
				Before: func(ctx *cli.Context) error {
					if ctx.Bool("debug") {
						log.SetLevel(logrus.DebugLevel)
					}
					return nil
				},
				Action: func(cCtx *cli.Context) error {
					opts := importOptions{
						dbname:      cCtx.String("dbname"),
						csvFilePath: cCtx.String("csv"),
					}

					if err := opts.validate(); err != nil {
						return err
					}
					return importEmails(cCtx.Context, log, opts)
				},
			},
			{
				Name:  "send",
				Usage: "send emails",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "debug", Aliases: []string{"d"}, Value: false, Usage: "enable debug output"},
					&cli.StringFlag{Name: "dbname", Usage: "local database name", Value: "emails.db"},
					&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "Config file to use", Required: true},
					&cli.IntFlag{Name: "count", Usage: "number of emails to send in this run", Value: 1000},
					&cli.BoolFlag{Name: "stop-on-error", Usage: "Flag to stop on error instead of sending the next email", Value: false},
				},
				Before: func(ctx *cli.Context) error {
					if ctx.Bool("debug") {
						log.SetLevel(logrus.DebugLevel)
					}
					return nil
				},
				Action: func(cCtx *cli.Context) error {
					configuration, err := config.GetConfig(cCtx.String("config"))
					if err != nil {
						return err
					}

					opts := sendOptions{
						dbname:         cCtx.String("dbname"),
						numberOfEmails: cCtx.Int("count"),
						stopOnError:    cCtx.Bool("stop-on-error"),
						config:         configuration,
					}

					if err := opts.validate(); err != nil {
						return err
					}
					return sendEmails(cCtx.Context, log, opts)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func importEmails(ctx context.Context, log *logrus.Logger, opts importOptions) error {
	if _, err := os.Stat(opts.dbname); err == nil {
		fmt.Println("Database already exists and will be removed by import. Press enter to continue, CTRL+C to cancel")
		fmt.Scanln()
	}

	if err := os.Remove(opts.dbname); err != nil {
		// ignore if no previous database
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	db, err := sql.Open("sqlite3", opts.dbname)
	if err != nil {
		return fmt.Errorf("could not open database: %w", err)
	}
	defer db.Close()

	sqlStmt := `
	CREATE TABLE emails (
		id INTEGER PRIMARY KEY,
		name TEXT,
		givenname TEXT,
		email TEXT NOT NULL UNIQUE,
		sent TEXT
	);
	`
	if _, err := db.Exec(sqlStmt); err != nil {
		return fmt.Errorf("could not create database table: %w", err)
	}

	csvFile, err := os.Open(opts.csvFilePath)
	if err != nil {
		return fmt.Errorf("error opening csv file: %w", err)
	}
	defer csvFile.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not create transaction: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, "insert into emails(name, givenname, email) values(?, ?, ?)")
	if err != nil {
		return fmt.Errorf("could not prepare database statement: %w", err)
	}
	defer stmt.Close()

	csvReader := csv.NewReader(csvFile)
	csvReader.FieldsPerRecord = 3
	csvReader.TrimLeadingSpace = true
	count := -1
	for {
		records, err := csvReader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if err2 := tx.Rollback(); err2 != nil {
				log.Error(err2)
			}
			return fmt.Errorf("error reading csv: %w", err)
		}

		count += 1
		// no need to process header line
		if count == 0 {
			continue
		}

		if _, err := stmt.ExecContext(ctx, records[0], records[1], records[2]); err != nil {
			if err2 := tx.Rollback(); err2 != nil {
				log.Error(err2)
			}
			return fmt.Errorf("could not execute insert statement with parameters %q, %q, %q: %w", records[0], records[1], records[2], err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error on commit: %w", err)
	}

	log.Infof("inserted %d records", count)

	return nil
}

func sendEmails(ctx context.Context, log *logrus.Logger, opts sendOptions) error {
	mail := mail.New(opts.config.Mail.Server, opts.config.Mail.Port,
		opts.config.Mail.User, opts.config.Mail.Password,
		opts.config.Mail.TLS, opts.config.Mail.SkipCertificateCheck,
		opts.config.Timeout.Duration)

	templateHTML, err := template.ParseFiles(opts.config.Templates.HTML)
	if err != nil {
		return fmt.Errorf("could not parse html template %s: %w", opts.config.Templates.HTML, err)
	}
	templateTXT, err := template.ParseFiles(opts.config.Templates.TXT)
	if err != nil {
		return fmt.Errorf("could not parse txt template %s: %w", opts.config.Templates.TXT, err)
	}

	db, err := sql.Open("sqlite3", opts.dbname)
	if err != nil {
		return fmt.Errorf("could not open database %s: %w", opts.dbname, err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "select id, name, givenname, email from emails where sent is null LIMIT ?", opts.numberOfEmails)
	if err != nil {
		return fmt.Errorf("could not execute query: %w", err)
	}
	defer rows.Close()

	// we need to store the emails in memory as sqlite does not allow for updating the entries while a select query is running :/
	var candidates []candidate
	for rows.Next() {
		var id int
		var name, givenname, email string
		if err := rows.Scan(&id, &name, &givenname, &email); err != nil {
			return fmt.Errorf("error on scanning rows: %w", err)
		}

		candidates = append(candidates, candidate{
			id:        id,
			name:      name,
			givenName: givenname,
			email:     email,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sql error: %w", err)
	}
	rows.Close()

	for _, candidate := range candidates {
		data := templateData{
			Name:      candidate.name,
			GivenName: candidate.givenName,
			Email:     candidate.email,
		}
		var tplHTML, tplTXT bytes.Buffer
		if err := templateHTML.Execute(&tplHTML, data); err != nil {
			return fmt.Errorf("could not execute html template: %w", err)
		}
		if err := templateTXT.Execute(&tplTXT, data); err != nil {
			return fmt.Errorf("could not execute TXT template: %w", err)
		}

		if err := mail.Send(opts.config.Mail.From.Name, opts.config.Mail.From.Mail, candidate.email, opts.config.Subject, tplHTML.String(), tplTXT.String()); err != nil {
			if opts.stopOnError {
				return fmt.Errorf("could not send email to %s: %w", candidate.email, err)
			}
			// continue with next email
			log.Errorf("could not send email to %s: %v. Continuing sending emails", candidate.email, err)
			continue
		}

		log.Debugf("sent email to %s", candidate.email)

		if _, err := db.ExecContext(ctx, "update emails set sent = datetime('now') where id = ?", candidate.id); err != nil {
			return fmt.Errorf("could not set sent date in database for email %s (email already sent): %w", candidate.email, err)
		}
	}

	var remainder int
	if err := db.QueryRowContext(ctx, "select count(*) from emails where sent is null").Scan(&remainder); err != nil {
		return fmt.Errorf("could not get remainder: %w", err)
	}

	log.Infof("Sent %d emails. %d emails left with no mail sent yet", len(candidates), remainder)

	return nil
}
