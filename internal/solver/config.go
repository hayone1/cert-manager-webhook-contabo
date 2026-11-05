package solver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const (
	// DefaultTTL is the TTL of the TXT records the webhook creates. The
	// records only live for the few minutes a challenge takes, so a short
	// TTL keeps resolvers from caching a stale answer.
	DefaultTTL = 60
	// MinTTL and MaxTTL bound the ttl setting.
	MinTTL = 1
	MaxTTL = 86400

	// Default key names inside the credentials Secret. They match the
	// external-dns Contabo webhook, so one Secret can serve both.
	DefaultClientIDKey     = "client-id"
	DefaultClientSecretKey = "client-secret"
	DefaultAPIUserKey      = "api-user"
	DefaultAPIPasswordKey  = "api-password"
)

// Config is the issuer's spec.acme.solvers[].dns01.webhook.config.
//
//	config:
//	  secretName: contabo-credentials   # required (unless ambient credentials are used)
//	  ttl: 60                           # optional
//	  zone: example.com                 # optional, normally found automatically
type Config struct {
	// SecretName is the Secret that holds the Contabo credentials. It is
	// read from the challenge's resource namespace: the Issuer's namespace,
	// or cert-manager's cluster resource namespace for a ClusterIssuer.
	SecretName string `json:"secretName,omitempty"`

	// SecretKeys changes the key names inside the Secret. Empty fields use
	// the defaults (client-id, client-secret, api-user, api-password).
	SecretKeys SecretKeys `json:"secretKeys,omitzero"`

	// TTL of the TXT record in seconds. Defaults to DefaultTTL.
	TTL *FlexInt `json:"ttl,omitempty"`

	// Zone is the Contabo zone to write to. Leave it empty: the webhook
	// picks the zone that matches what cert-manager found in DNS. Set it
	// only if that guess is wrong for your setup.
	Zone string `json:"zone,omitempty"`

	// There is deliberately no setting for the API URL. An issuer author
	// could otherwise make the webhook send credentials, or requests from
	// inside the cluster, to any address. Tests set CONTABO_API_URL and
	// CONTABO_AUTH_URL on the webhook instead.
}

// SecretKeys names the keys inside the credentials Secret.
type SecretKeys struct {
	ClientID     string `json:"clientID,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	APIUser      string `json:"apiUser,omitempty"`
	APIPassword  string `json:"apiPassword,omitempty"`
}

// FlexInt is a whole number that may also be written as a string ("60"),
// because YAML users often quote numbers.
type FlexInt int64

// UnmarshalJSON implements json.Unmarshaler.
func (n *FlexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		b = []byte(strings.TrimSpace(s))
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a whole number", string(b))
	}
	*n = FlexInt(v)
	return nil
}

// parseConfig decodes and checks the issuer config. Unknown fields are an
// error, so a typo such as "secretname" is reported instead of ignored.
func parseConfig(raw *extapi.JSON) (Config, error) {
	var cfg Config
	if raw != nil && len(bytes.TrimSpace(raw.Raw)) > 0 && string(bytes.TrimSpace(raw.Raw)) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw.Raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("invalid webhook config: %w", err)
		}
		if dec.More() {
			return cfg, errors.New("invalid webhook config: unexpected data after the JSON object")
		}
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var errs []error
	c.SecretName = strings.TrimSpace(c.SecretName)
	if c.SecretName != "" && !validSecretName(c.SecretName) {
		errs = append(errs, fmt.Errorf("secretName %q is not a valid Kubernetes Secret name", c.SecretName))
	}
	if c.TTL != nil && (*c.TTL < MinTTL || *c.TTL > MaxTTL) {
		errs = append(errs, fmt.Errorf("ttl must be between %d and %d seconds, got %d", MinTTL, MaxTTL, *c.TTL))
	}
	if c.Zone != "" {
		z, err := normalizeName(c.Zone)
		if err != nil || z == "" || !strings.Contains(z, ".") {
			errs = append(errs, fmt.Errorf("zone %q is not a valid domain name", c.Zone))
		}
		c.Zone = z
	}
	for name, k := range map[string]string{
		"secretKeys.clientID": c.SecretKeys.ClientID, "secretKeys.clientSecret": c.SecretKeys.ClientSecret,
		"secretKeys.apiUser": c.SecretKeys.APIUser, "secretKeys.apiPassword": c.SecretKeys.APIPassword,
	} {
		if k != "" && !validSecretKey(k) {
			errs = append(errs, fmt.Errorf("%s %q is not a valid Secret key", name, k))
		}
	}
	return errors.Join(errs...)
}

func (c *Config) ttl() int64 {
	if c.TTL == nil {
		return DefaultTTL
	}
	return int64(*c.TTL)
}

func (k SecretKeys) withDefaults() SecretKeys {
	if k.ClientID == "" {
		k.ClientID = DefaultClientIDKey
	}
	if k.ClientSecret == "" {
		k.ClientSecret = DefaultClientSecretKey
	}
	if k.APIUser == "" {
		k.APIUser = DefaultAPIUserKey
	}
	if k.APIPassword == "" {
		k.APIPassword = DefaultAPIPasswordKey
	}
	return k
}

// validSecretName checks the DNS-1123 subdomain rule that Kubernetes uses
// for Secret names.
func validSecretName(s string) bool {
	if len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// validSecretKey checks the rule Kubernetes uses for keys in a Secret.
func validSecretKey(s string) bool {
	if len(s) > 253 || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}
