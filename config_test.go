package growwapi

import (
	"flag"
	"strings"
	"testing"
)

func TestConfig_Precedence(t *testing.T) {
	yaml := []byte("access_token: from-file\nhttp_timeout: 99\n")
	fs := func(path string) ([]byte, error) { return yaml, nil }
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(&strings.Builder{})

	cfg := NewConfig()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if err := cfg.Load(LoadOptions{
		Args:    []string{"-access-token", "from-flag", "-config", "fake.yaml"},
		Env:     []string{"GROWW_ACCESS_TOKEN=from-env", "GROWW_API_BASE_URL=https://x"},
		FS:      fs,
		FlagSet: flags,
	}); err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := cfg.AccessToken; got != "from-flag" {
		t.Errorf("AccessToken = %q want from-flag", got)
	}
	src, _ := cfg.SourceOf("access_token")
	if src != ConfigSourceFlag {
		t.Errorf("source = %s want flag", src)
	}

	if got := cfg.APIBaseURL; got != "https://x" {
		t.Errorf("APIBaseURL = %q want https://x", got)
	}
	if src, _ := cfg.SourceOf("api_base_url"); src != ConfigSourceEnv {
		t.Errorf("api_base_url source = %s want env", src)
	}

	// http_timeout comes only from the YAML file in this run.
	if got := cfg.HTTPTimeoutSeconds; got != 99 {
		t.Errorf("HTTPTimeoutSeconds = %d want 99", got)
	}
	if src, _ := cfg.SourceOf("http_timeout"); src != ConfigSourceFile {
		t.Errorf("http_timeout source = %s want file", src)
	}
}

func TestConfig_Default(t *testing.T) {
	cfg := NewConfig()
	if got := cfg.APIBaseURL; got != "https://api.groww.in" {
		t.Errorf("default api base url = %q", got)
	}
	if got := cfg.HTTPTimeoutSeconds; got != 30 {
		t.Errorf("default http timeout = %d", got)
	}
	if src, _ := cfg.SourceOf("api_base_url"); src != ConfigSourceDefault {
		t.Errorf("api_base_url source = %s want default", src)
	}
}

func TestConfig_MissingAccessToken(t *testing.T) {
	cfg := NewConfig()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(&strings.Builder{})
	// Pass empty args so go test's own flags don't pollute the set.
	err := cfg.Load(LoadOptions{
		Args:    []string{},
		Env:     nil,
		FS:      func(string) ([]byte, error) { return nil, nil },
		FlagSet: flags,
	})
	if err == nil {
		t.Fatalf("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "GROWW_ACCESS_TOKEN") {
		t.Errorf("error must mention token: %v", err)
	}
}

func TestConfig_SourcesMap(t *testing.T) {
	cfg := NewConfig()
	if got := cfg.Sources()["access_token"]; got != ConfigSourceDefault {
		t.Errorf("want default source for access_token, got %s", got)
	}
}
