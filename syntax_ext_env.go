package directivefile

import (
	"fmt"
	"os"
)

// envNamePattern follows the POSIX shell environment-variable naming style.
const envNamePattern = `[A-Za-z_][A-Za-z0-9_]*`

// parseEnvReference parses an environment-variable reference of the form
// `{$NAME}`, where NAME must match [A-Za-z_][A-Za-z0-9_]*. It returns NAME,
// or an error if the reference is malformed.
func parseEnvReference(reference string) (string, error) {
	if len(reference) < 3 || reference[0] != '{' || reference[1] != '$' || reference[len(reference)-1] != '}' {
		return "", fmt.Errorf("invalid environment variable: %s", reference)
	}

	name := reference[2 : len(reference)-1]
	if len(name) == 0 {
		return "", fmt.Errorf("invalid environment variable: %s", reference)
	}

	if !(('a' <= name[0] && name[0] <= 'z') ||
		('A' <= name[0] && name[0] <= 'Z') ||
		name[0] == '_') {
		return "", fmt.Errorf("invalid environment variable: %s (NAME must match %s)", reference, envNamePattern)
	}

	for i := 1; i < len(name); i++ {
		c := name[i]
		if !(('a' <= c && c <= 'z') ||
			('A' <= c && c <= 'Z') ||
			('0' <= c && c <= '9') ||
			c == '_') {
			return "", fmt.Errorf("invalid environment variable: %s (NAME must match %s)", reference, envNamePattern)
		}
	}

	return name, nil
}

// lookupEnv returns the value of name. It reports an error if the variable is
// unset or set to an empty value.
func lookupEnv(name string) (string, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	if value == "" {
		return "", fmt.Errorf("environment variable %q is empty", name)
	}
	return value, nil
}
