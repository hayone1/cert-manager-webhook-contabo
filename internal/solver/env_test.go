package solver

import (
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestOptionsFromEnvDefaults(t *testing.T) {
	o, err := OptionsFromEnv(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if o.AmbientCredentials != nil || o.OperationTimeout != DefaultOperationTimeout ||
		o.RateLimit != DefaultRateLimit || o.MaxRetries != DefaultMaxRetries || o.APIURL != "" {
		t.Fatalf("unexpected defaults: %+v", o)
	}
	s := New(o)
	if s.opts.APIURL != DefaultAPIURL || s.opts.AuthURL != DefaultAuthURL {
		t.Fatalf("endpoints: %s %s", s.opts.APIURL, s.opts.AuthURL)
	}
}

func TestOptionsFromEnvValues(t *testing.T) {
	o, err := OptionsFromEnv(envOf(map[string]string{
		EnvAPIURL:           "http://fake:8080",
		EnvAuthURL:          "http://fake:8080/token",
		EnvOperationTimeout: "30s",
		EnvRequestTimeout:   "5s",
		EnvRateLimit:        "5",
		EnvMaxRetries:       "0",
		EnvClientID:         " INT-1 ",
		EnvClientSecret:     "secret\n",
		EnvAPIUser:          "me@example.com",
		EnvAPIPassword:      " pass word ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if o.OperationTimeout != 30*time.Second || o.RequestTimeout != 5*time.Second || o.RateLimit != 5 || o.MaxRetries != 0 {
		t.Fatalf("values: %+v", o)
	}
	c := o.AmbientCredentials
	if c == nil || c.ClientID != "INT-1" || c.ClientSecret != "secret" || c.APIPassword != " pass word " {
		t.Fatalf("ambient: %+v", c)
	}
}

func TestOptionsFromEnvErrors(t *testing.T) {
	_, err := OptionsFromEnv(envOf(map[string]string{
		EnvAPIURL:           "ftp://x",
		EnvOperationTimeout: "1s",
		EnvRequestTimeout:   "soon",
		EnvRateLimit:        "100",
		EnvMaxRetries:       "-1",
		EnvClientID:         "INT-1",
	}))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{EnvAPIURL, EnvOperationTimeout, EnvRequestTimeout, EnvRateLimit, EnvMaxRetries, "ambient credentials are incomplete"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "INT-1") {
		t.Fatal("error must not print credential values")
	}
}
