package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const defaultBaseURL = "https://gottem.link"

type command struct {
	name        string
	slug        string
	slugSet     bool
	destination string
	force       bool
	json        bool
	baseURL     *url.URL
}

func parseCommand(args []string) (command, error) {
	var result command
	baseRaw := os.Getenv("GOTTEM_BASE_URL")
	if baseRaw == "" {
		baseRaw = defaultBaseURL
	}

	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch {
		case args[0] == "--json":
			result.json = true
			args = args[1:]
		case args[0] == "--base-url":
			if len(args) < 2 {
				return result, errors.New("--base-url requires a URL")
			}
			baseRaw = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--base-url="):
			baseRaw = strings.TrimPrefix(args[0], "--base-url=")
			args = args[1:]
		default:
			return result, fmt.Errorf("unknown global flag %q", args[0])
		}
	}
	if len(args) == 0 {
		return result, errors.New("missing command")
	}

	result.name = args[0]
	commandArgs := args[1:]
	var err error
	switch result.name {
	case "create":
		err = parseCreate(&result, commandArgs)
	case "list":
		err = requireArguments(commandArgs, 0, "list takes no arguments")
	case "get", "disable":
		if err = requireArguments(commandArgs, 1, result.name+" requires exactly one slug"); err == nil {
			result.slug = commandArgs[0]
			err = requireNonEmpty(result.slug, "slug")
		}
	case "update":
		if err = requireArguments(commandArgs, 2, "update requires SLUG and URL"); err == nil {
			result.slug, result.destination = commandArgs[0], commandArgs[1]
			if err = requireNonEmpty(result.slug, "slug"); err == nil {
				err = requireNonEmpty(result.destination, "URL")
			}
		}
	case "delete":
		err = parseDelete(&result, commandArgs)
	default:
		err = fmt.Errorf("unknown command %q", result.name)
	}
	if err != nil {
		return result, err
	}
	result.baseURL, err = parseBaseURL(baseRaw)
	if err != nil {
		return result, fmt.Errorf("invalid base URL: %w", err)
	}
	return result, nil
}

func parseCreate(result *command, args []string) error {
	if len(args) >= 1 && args[0] == "--slug" {
		if len(args) < 2 {
			return errors.New("--slug requires a value")
		}
		result.slug = args[1]
		result.slugSet = true
		args = args[2:]
	} else if len(args) >= 1 && strings.HasPrefix(args[0], "--slug=") {
		result.slug = strings.TrimPrefix(args[0], "--slug=")
		result.slugSet = true
		args = args[1:]
	} else if len(args) >= 1 && strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unknown create flag %q", args[0])
	}
	if err := requireArguments(args, 1, "create requires exactly one URL"); err != nil {
		return err
	}
	if result.slugSet {
		if err := requireNonEmpty(result.slug, "slug"); err != nil {
			return err
		}
	}
	result.destination = args[0]
	return requireNonEmpty(result.destination, "URL")
}

func parseDelete(result *command, args []string) error {
	if len(args) >= 1 && args[0] == "--force" {
		result.force = true
		args = args[1:]
	} else if len(args) >= 1 && strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unknown delete flag %q", args[0])
	}
	if err := requireArguments(args, 1, "delete requires exactly one slug"); err != nil {
		return err
	}
	result.slug = args[0]
	return requireNonEmpty(result.slug, "slug")
}

func requireArguments(args []string, count int, message string) error {
	if len(args) != count {
		return errors.New(message)
	}
	return nil
}

func requireNonEmpty(value, name string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("must not be empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("must be an absolute HTTP or HTTPS origin")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, errors.New("scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("must be an origin without userinfo, query, or fragment")
	}
	if port := parsed.Port(); port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return nil, errors.New("port must be between 0 and 65535")
		}
	}
	if escaped := parsed.EscapedPath(); escaped != "" && escaped != "/" {
		return nil, errors.New("path must be empty or /")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed, nil
}
