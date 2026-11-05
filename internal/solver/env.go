package solver

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo"
)

// Environment variables the webhook reads at start. They are set by the
// cluster admin (through the Helm chart), never by an issuer. These are
// variable names, not credentials.
//
//nolint:gosec // G101: names of environment variables
const (
	EnvAPIURL           = "CONTABO_API_URL"
	EnvAuthURL          = "CONTABO_AUTH_URL"
	EnvClientID         = "CONTABO_CLIENT_ID"
	EnvClientSecret     = "CONTABO_CLIENT_SECRET"
	EnvAPIUser          = "CONTABO_API_USER"
	EnvAPIPassword      = "CONTABO_API_PASSWORD"
	EnvOperationTimeout = "CONTABO_OPERATION_TIMEOUT"
	EnvRequestTimeout   = "CONTABO_REQUEST_TIMEOUT"
	EnvRateLimit        = "CONTABO_RATE_LIMIT"
	EnvMaxRetries       = "CONTABO_MAX_RETRIES"
)

// Defaults for the environment settings.
const (
	DefaultOperationTimeout = 45 * time.Second
	DefaultRequestTimeout   = 15 * time.Second
	DefaultRateLimit        = 10.0
	DefaultMaxRetries       = 4
)

// OptionsFromEnv reads the solver options from environment variables.
// getenv is os.Getenv in production.
func OptionsFromEnv(getenv func(string) string) (Options, error) {
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	var errs []error
	o := Options{
		APIURL:           get(EnvAPIURL),
		AuthURL:          get(EnvAuthURL),
		OperationTimeout: DefaultOperationTimeout,
		RequestTimeout:   DefaultRequestTimeout,
		RateLimit:        DefaultRateLimit,
		MaxRetries:       DefaultMaxRetries,
	}
	for k, v := range map[string]string{EnvAPIURL: o.APIURL, EnvAuthURL: o.AuthURL} {
		if v == "" {
			continue
		}
		if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be an http or https URL, got %q", k, v))
		}
	}

	dur := func(k string, def, minimum, maximum time.Duration) time.Duration {
		v := get(k)
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil || d < minimum || d > maximum {
			errs = append(errs, fmt.Errorf("%s must be a duration between %s and %s, got %q", k, minimum, maximum, v))
			return def
		}
		return d
	}
	o.OperationTimeout = dur(EnvOperationTimeout, DefaultOperationTimeout, 5*time.Second, 10*time.Minute)
	o.RequestTimeout = dur(EnvRequestTimeout, DefaultRequestTimeout, time.Second, 5*time.Minute)

	if v := get(EnvRateLimit); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 15 {
			errs = append(errs, fmt.Errorf("%s must be a number above 0 and at most 15 (Contabo's limit), got %q", EnvRateLimit, v))
		} else {
			o.RateLimit = f
		}
	}
	if v := get(EnvMaxRetries); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 20 {
			errs = append(errs, fmt.Errorf("%s must be a whole number from 0 to 20, got %q", EnvMaxRetries, v))
		} else {
			o.MaxRetries = n
		}
	}

	creds := contabo.Credentials{
		ClientID:     get(EnvClientID),
		ClientSecret: strings.Trim(getenv(EnvClientSecret), "\r\n"),
		APIUser:      get(EnvAPIUser),
		APIPassword:  strings.Trim(getenv(EnvAPIPassword), "\r\n"),
	}
	set := map[string]bool{
		EnvClientID: creds.ClientID != "", EnvClientSecret: creds.ClientSecret != "",
		EnvAPIUser: creds.APIUser != "", EnvAPIPassword: creds.APIPassword != "",
	}
	var have, missing []string
	for _, k := range []string{EnvClientID, EnvClientSecret, EnvAPIUser, EnvAPIPassword} {
		if set[k] {
			have = append(have, k)
		} else {
			missing = append(missing, k)
		}
	}
	switch {
	case len(missing) == 0:
		o.AmbientCredentials = &creds
	case len(have) > 0:
		errs = append(errs, fmt.Errorf("ambient credentials are incomplete: %s set but %s missing",
			strings.Join(have, ", "), strings.Join(missing, ", ")))
	}
	return o, errors.Join(errs...)
}
