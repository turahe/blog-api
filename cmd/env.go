package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadEnvFile reads KEY=VALUE lines into the process environment. Existing
// variables win so Docker/K8s/shell exports are never overridden. Blank lines
// and # comments are ignored. Values may be single- or double-quoted.
func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, skip, err := parseEnvLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("%q:%d: %w", path, lineNo, err)
		}
		if skip {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%q:%d: set %q: %w", path, lineNo, key, err)
		}
	}
	return scanner.Err()
}

// parseEnvLine returns key/value for an assignment line. skip is true for
// blanks and comments. The error is a short reason without path/line context.
func parseEnvLine(raw string) (key, value string, skip bool, err error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", true, nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false, fmt.Errorf("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, fmt.Errorf("empty key")
	}
	value = unquoteEnvValue(strings.TrimSpace(value))
	return key, value, false, nil
}

func unquoteEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	doubleQuoted := value[0] == '"' && value[len(value)-1] == '"'
	singleQuoted := value[0] == '\'' && value[len(value)-1] == '\''
	if doubleQuoted || singleQuoted {
		return value[1 : len(value)-1]
	}
	return value
}

// resolveEnvFile picks the dotenv path. Explicit --env-file must exist.
// Pass "-" to disable loading. With the default (empty), load .env when
// present; otherwise skip.
func resolveEnvFile(flag string) (string, error) {
	switch flag {
	case "-":
		return "", nil
	case "":
		_, err := os.Stat(".env")
		if err == nil {
			return ".env", nil
		}
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("env file %q: %w", ".env", err)
	default:
		if _, err := os.Stat(flag); err != nil {
			return "", fmt.Errorf("env file %q: %w", flag, err)
		}
		return flag, nil
	}
}
