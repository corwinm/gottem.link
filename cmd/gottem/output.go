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
		value, requestErr := client.create(ctx, command.slug, command.destination)
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
  gottem [--base-url URL] [--json] create [--slug SLUG] URL
  gottem [--base-url URL] [--json] list
  gottem [--base-url URL] [--json] get SLUG
  gottem [--base-url URL] [--json] update SLUG URL
  gottem [--base-url URL] [--json] disable SLUG
  gottem [--base-url URL] [--json] delete [--force] SLUG
`

func writeCreate(writer io.Writer, command command, value redirect) error {
	if command.json {
		return writeJSON(writer, value)
	}
	_, err := fmt.Fprintf(writer, "Created %s/%s -> %s\n", command.baseURL.String(), value.Slug, value.URL)
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
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", value.Slug, redirectStatus(value), value.URL); err != nil {
			return err
		}
	}
	return table.Flush()
}

func writeGet(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	if _, err := fmt.Fprintf(writer, "Slug: %s\nURL: %s\nStatus: %s\nCreated: %s\nUpdated: %s\n", value.Slug, value.URL, redirectStatus(value), value.CreatedAt, value.UpdatedAt); err != nil {
		return err
	}
	if value.DisabledAt != nil {
		_, err := fmt.Fprintf(writer, "Disabled: %s\n", *value.DisabledAt)
		return err
	}
	return nil
}

func writeUpdate(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	_, err := fmt.Fprintf(writer, "Updated %s -> %s\n", value.Slug, value.URL)
	return err
}

func writeDisable(writer io.Writer, jsonOutput bool, value redirect) error {
	if jsonOutput {
		return writeJSON(writer, value)
	}
	_, err := fmt.Fprintf(writer, "Disabled %s\n", value.Slug)
	return err
}

func writeDelete(writer io.Writer, jsonOutput bool, slug string) error {
	if jsonOutput {
		return writeJSON(writer, struct {
			Deleted bool   `json:"deleted"`
			Slug    string `json:"slug"`
		}{Deleted: true, Slug: slug})
	}
	_, err := fmt.Fprintf(writer, "Deleted %s\n", slug)
	return err
}

func writeJSON(writer io.Writer, value any) error {
	return json.NewEncoder(writer).Encode(value)
}

func redirectStatus(value redirect) string {
	if value.DisabledAt != nil {
		return "disabled"
	}
	return "active"
}
