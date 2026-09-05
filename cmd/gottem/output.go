package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"corwinm/gottem.link/backup"
)

type confirmFunc func(slug string) (bool, error)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, httpClient *http.Client, confirm confirmFunc) int {
	command, err := parseCommand(args)
	if err != nil {
		fmt.Fprintf(stderr, "gottem: %s\n%s", sanitize(err.Error()), usageText)
		return 2
	}
	token := os.Getenv("GOTTEM_MANAGEMENT_TOKEN")
	if token == "" {
		fmt.Fprintln(stderr, "gottem: GOTTEM_MANAGEMENT_TOKEN is required")
		return 2
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if command.name == "delete" && !command.force {
		if confirm == nil {
			confirm = func(slug string) (bool, error) {
				return terminalConfirm(stdin, stderr, slug)
			}
		}
		confirmed, confirmErr := confirm(command.slug)
		if confirmErr != nil {
			writeDiagnostic(stderr, confirmErr, token)
			return 1
		}
		if !confirmed {
			fmt.Fprintln(stderr, "Delete cancelled.")
			return 1
		}
	}

	client := managementClient{baseURL: command.baseURL, token: token, http: httpClient}
	ctx := context.Background()
	switch command.name {
	case "create":
		value, requestErr := client.create(ctx, command.slug, command.destination, command.expiresAt)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeCreate(stdout, command, value)
		}
	case "list":
		values, requestErr := client.list(ctx)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeList(stdout, command.json, values)
		}
	case "export":
		envelope, requestErr := client.export(ctx)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeExport(stdout, envelope)
		}
	case "import":
		var reader io.Reader = stdin
		var file *os.File
		if command.file != "-" {
			file, err = os.Open(command.file)
			if err != nil {
				err = errors.New("open import file")
				break
			}
			defer file.Close()
			reader = file
		}
		envelope, decodeErr := backup.Decode(reader)
		if decodeErr != nil {
			err = decodeErr
			break
		}
		value, requestErr := client.importRedirects(ctx, envelope, !command.apply)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeImport(stdout, command.json, command.apply, value)
		}
	case "get":
		value, requestErr := client.get(ctx, command.slug)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeGet(stdout, command.json, value)
		}
	case "update":
		value, requestErr := client.update(ctx, command.slug, command.destination)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeUpdate(stdout, command.json, value)
		}
	case "disable":
		value, requestErr := client.disable(ctx, command.slug)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeDisable(stdout, command.json, value)
		}
	case "expire", "unexpire":
		var expiresAt *string
		if command.name == "expire" {
			expiresAt = &command.expiresAt
		}
		value, requestErr := client.setExpiration(ctx, command.slug, expiresAt)
		if requestErr != nil {
			err = requestErr
		} else {
			err = writeExpiration(stdout, command.json, value)
		}
	case "delete":
		if requestErr := client.delete(ctx, command.slug); requestErr != nil {
			err = requestErr
		} else {
			err = writeDelete(stdout, command.json, command.slug)
		}
	}
	if err != nil {
		writeDiagnostic(stderr, err, token)
		return 1
	}
	return 0
}

func terminalConfirm(stdin io.Reader, stderr io.Writer, slug string) (bool, error) {
	file, ok := stdin.(interface{ Stat() (os.FileInfo, error) })
	if !ok {
		return false, errors.New("delete requires an interactive terminal; use --force")
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false, errors.New("delete requires an interactive terminal; use --force")
	}
	return promptConfirm(stdin, stderr, slug)
}

func promptConfirm(stdin io.Reader, stderr io.Writer, slug string) (bool, error) {
	fmt.Fprintf(stderr, "Delete redirect %q permanently? [y/N] ", slug)
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func writeDiagnostic(writer io.Writer, err error, token string) {
	message := sanitize(err.Error())
	if token != "" {
		message = strings.ReplaceAll(message, token, "[redacted]")
	}
	fmt.Fprintf(writer, "gottem: %s\n", message)
}

const usageText = `Usage:
  gottem [--base-url URL] [--json] create [--slug SLUG] [--expires-at RFC3339] URL
  gottem [--base-url URL] [--json] list
  gottem [--base-url URL] [--json] export
  gottem [--base-url URL] [--json] import [--apply] FILE
  gottem [--base-url URL] [--json] get SLUG
  gottem [--base-url URL] [--json] update SLUG URL
  gottem [--base-url URL] [--json] disable SLUG
  gottem [--base-url URL] [--json] expire SLUG RFC3339
  gottem [--base-url URL] [--json] unexpire SLUG
  gottem [--base-url URL] [--json] delete [--force] SLUG
`

func writeCreate(writer io.Writer, command command, value redirect) error {
	if command.json {
		return writeJSON(writer, value)
	}
	_, err := fmt.Fprintf(writer, "Created %s/%s -> %s\n", sanitize(command.baseURL.String()), sanitize(value.Slug), sanitize(value.URL))
	return err
}

func writeList(writer io.Writer, jsonOutput bool, values []redirect) error {
	if jsonOutput {
		return writeJSON(writer, values)
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(writer, "No redirects.")
		return err
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "SLUG\tSTATUS\tDESTINATION"); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(table, "%s	%s	%s\n", sanitize(value.Slug), redirectStatus(value), sanitize(value.URL)); err != nil {
			return err
		}
	}
	return table.Flush()
}

func writeExport(writer io.Writer, envelope backup.Envelope) error {
	return writeJSON(writer, envelope)
}

func writeImport(writer io.Writer, jsonOutput, apply bool, value importResult) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	if apply {
		_, err := fmt.Fprintf(writer, "Imported %d redirects.\n", value.Imported)
		return err
	}
	_, err := fmt.Fprintf(writer, "Validated %d redirects; no changes applied.\n", value.Total)
	return err
}

func writeGet(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	if _, err := fmt.Fprintf(writer, "Slug: %s\nURL: %s\nStatus: %s\nCreated: %s\nUpdated: %s\n", sanitize(value.Slug), sanitize(value.URL), redirectStatus(value), sanitize(value.CreatedAt), sanitize(value.UpdatedAt)); err != nil {
		return err
	}
	if value.DestinationUpdatedAt != "" {
		if _, err := fmt.Fprintf(writer, "Destination updated: %s\n", sanitize(value.DestinationUpdatedAt)); err != nil {
			return err
		}
	}
	if value.DisabledAt != nil {
		if _, err := fmt.Fprintf(writer, "Disabled: %s\n", sanitize(*value.DisabledAt)); err != nil {
			return err
		}
	}
	if value.ExpiresAt != nil {
		_, err := fmt.Fprintf(writer, "Expires: %s\n", sanitize(*value.ExpiresAt))
		return err
	}
	return nil
}

func writeUpdate(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	_, err := fmt.Fprintf(writer, "Updated %s -> %s\n", sanitize(value.Slug), sanitize(value.URL))
	return err
}

func writeDisable(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	_, err := fmt.Fprintf(writer, "Disabled %s\n", sanitize(value.Slug))
	return err
}

func writeExpiration(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	if value.ExpiresAt == nil {
		_, err := fmt.Fprintf(writer, "Cleared expiration for %s\n", sanitize(value.Slug))
		return err
	}
	_, err := fmt.Fprintf(writer, "Expires %s at %s\n", sanitize(value.Slug), sanitize(*value.ExpiresAt))
	return err
}

func writeDelete(writer io.Writer, jsonOutput bool, slug string) error {
	if jsonOutput {
		return writeJSON(writer, struct {
			Deleted bool   `json:"deleted"`
			Slug    string `json:"slug"`
		}{Deleted: true, Slug: slug})
	}
	_, err := fmt.Fprintf(writer, "Deleted %s\n", sanitize(slug))
	return err
}

func writeJSON(writer io.Writer, value any) error {
	return json.NewEncoder(writer).Encode(value)
}

func redirectStatus(value redirect) string {
	if value.DisabledAt != nil {
		return "disabled"
	}
	if value.ExpiresAt != nil {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
			if expiresAt, err := time.Parse(layout, *value.ExpiresAt); err == nil && !expiresAt.After(time.Now()) {
				return "expired"
			}
		}
	}
	return "active"
}
