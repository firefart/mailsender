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
	dbname           string
	templatePathHTML string
	templatePathTXT  string
	fromFriendlyName string
	fromEmail        string
	subject          string
	numberOfEmails   int
	mail             struct {
		host     string
		port     int
		username string
		password string
		skipTLS  bool
	}
}

func (o sendOptions) validate() error {
	if o.dbname == "" {
		return fmt.Errorf("please set a database name")
	}

	if o.templatePathHTML == "" {
		return fmt.Errorf("please set a html template path")
	}

	if o.templatePathTXT == "" {
		return fmt.Errorf("please set a txt template path")
	}

	if o.fromFriendlyName == "" {
		return fmt.Errorf("please set a friendly from name")
	}

	if o.fromEmail == "" {
		return fmt.Errorf("please set a from email")
	}

	if o.subject == "" {
		return fmt.Errorf("please set a subject")
	}

	if o.mail.host == "" {
		return fmt.Errorf("please set a mail host")
	}

	if o.mail.port == 0 {
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
					&cli.StringFlag{Name: "html-template", Usage: "HTML template to use for email", Required: true},
					&cli.StringFlag{Name: "text-template", Usage: "TXT template to use for email", Required: true},
					&cli.StringFlag{Name: "subject", Usage: "subject to use for the email", Required: true},
					&cli.StringFlag{Name: "friendlyfrom", Usage: "friendly name to set in from", Required: true},
					&cli.StringFlag{Name: "from", Usage: "the from email address", Required: true},
					&cli.IntFlag{Name: "count", Usage: "number of emails to send in this run", Value: 1000},
					&cli.StringFlag{Name: "mailserver", Usage: "mail server to use", Required: true},
					&cli.IntFlag{Name: "mailport", Usage: "mailserver port to use", Value: 25},
					&cli.StringFlag{Name: "username", Usage: "username to authenticate against the mailserver if needed. Can be left blank"},
					&cli.StringFlag{Name: "password", Usage: "password to authenticate against the mailserver if needed. Can be left blank"},
					&cli.BoolFlag{Name: "skiptls", Usage: "skip tls validation when connecting to the mailserver", Value: false},
				},
				Before: func(ctx *cli.Context) error {
					if ctx.Bool("debug") {
						log.SetLevel(logrus.DebugLevel)
					}
					return nil
				},
				Action: func(cCtx *cli.Context) error {
					opts := sendOptions{
						dbname:           cCtx.String("dbname"),
						templatePathHTML: cCtx.String("html-template"),
						templatePathTXT:  cCtx.String("text-template"),
						fromFriendlyName: cCtx.String("friendlyfrom"),
						fromEmail:        cCtx.String("from"),
						subject:          cCtx.String("subject"),
						numberOfEmails:   cCtx.Int("count"),
					}
					opts.mail.host = cCtx.String("mailserver")
					opts.mail.port = cCtx.Int("mailport")
					opts.mail.username = cCtx.String("username")
					opts.mail.password = cCtx.String("password")
					opts.mail.skipTLS = cCtx.Bool("skiptls")

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
	mail := mail.New(opts.mail.host, opts.mail.port, opts.mail.username, opts.mail.password, opts.mail.skipTLS)

	templateHTML, err := template.ParseFiles(opts.templatePathHTML)
	if err != nil {
		return fmt.Errorf("could not parse html template %s: %w", opts.templatePathHTML, err)
	}
	templateTXT, err := template.ParseFiles(opts.templatePathTXT)
	if err != nil {
		return fmt.Errorf("could not parse txt template %s: %w", opts.templatePathTXT, err)
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

		if err := mail.Send(opts.fromFriendlyName, opts.fromEmail, candidate.email, opts.subject, tplHTML.String(), tplTXT.String()); err != nil {
			return fmt.Errorf("could not send email to %s: %w", candidate.email, err)
		}

		log.Debugf("send email to %s", candidate.email)

		if _, err := db.ExecContext(ctx, "update emails set sent = datetime('now') where id = ?", candidate.id); err != nil {
			return fmt.Errorf("could not set sent date in database for email %s: %w", candidate.email, err)
		}
	}

	log.Infof("Sent %d emails. There might be still emails in the database to send emails to", len(candidates))

	return nil
}
