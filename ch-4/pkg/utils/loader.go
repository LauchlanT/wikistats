package utils

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Load values from a file into the environment
func LoadEnv(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err = errors.Join(err, file.Close())
	}(file)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Find lines of form KEY=VALUE
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setting %s=%s: %w", key, value, err)
		}
	}

	return scanner.Err()
}
