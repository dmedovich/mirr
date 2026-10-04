package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"mirr/internal/config"
)

func Secret(ctx context.Context, c config.Credential) (string, error) {
	if c.Provider == "env" {
		v := os.Getenv(c.Variable)
		if v == "" {
			return "", fmt.Errorf("credential environment variable %s is empty", c.Variable)
		}
		return v, nil
	}
	b, e := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "mirr", "account", c.Account).Output()
	if e != nil {
		return "", fmt.Errorf("keyring unavailable or account %s missing; use mirr auth set", c.Account)
	}
	v := strings.TrimSuffix(string(b), "\n")
	if v == "" {
		return "", fmt.Errorf("keyring secret is empty")
	}
	return v, nil
}

func Store(ctx context.Context, account, secret string) error {
	if secret == "" {
		return fmt.Errorf("empty secret")
	}
	cmd := exec.CommandContext(ctx, "secret-tool", "store", "--label=mirr: "+account, "service", "mirr", "account", account)
	cmd.Stdin = strings.NewReader(secret)
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("could not store secret: check secret-tool and unlocked Secret Service: %w", e)
	}
	return nil
}
