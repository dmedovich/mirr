package auth

import (
	"context"
	"testing"

	"mirr/internal/config"
)

func TestEnvironmentCredential(t *testing.T) {
	t.Setenv("MIRR_TEST_TOKEN", "test-only-value")
	got, e := Secret(context.Background(), config.Credential{Provider: "env", Variable: "MIRR_TEST_TOKEN"})
	if e != nil || got != "test-only-value" {
		t.Fatal("could not resolve environment credential")
	}
	t.Setenv("MIRR_TEST_TOKEN", "")
	if _, e = Secret(context.Background(), config.Credential{Provider: "env", Variable: "MIRR_TEST_TOKEN"}); e == nil {
		t.Fatal("empty credential accepted")
	}
}
