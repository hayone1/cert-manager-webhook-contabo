// Command webhook is the cert-manager DNS-01 webhook for Contabo DNS.
//
// cert-manager calls it through the Kubernetes API server (it is an
// aggregated API server) to add and remove _acme-challenge TXT records.
//
// Settings come from environment variables. GROUP_NAME is required and must
// match the groupName in your issuers. See docs/configuration.md.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	logf "github.com/cert-manager/cert-manager/pkg/logs"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/solver"
)

// Set at build time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Printf("cert-manager-webhook-contabo %s (commit %s)\n", version, commit)
		return
	}

	groupName := strings.TrimSpace(os.Getenv("GROUP_NAME"))
	if err := checkGroupName(groupName); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	opts, err := solver.OptionsFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: invalid settings:", err)
		os.Exit(2)
	}
	opts.UserAgent = "cert-manager-webhook-contabo/" + version
	// Log through cert-manager's logger, so the webhook's own lines use the
	// same format and verbosity flags (-v) as the server around it.
	opts.Logger = newSlogLogger(logf.Log.WithName("contabo"))

	cmd.RunWebhookServer(groupName, solver.New(opts))
}

// checkGroupName makes sure GROUP_NAME looks like a DNS name, because it
// becomes the API group "v1alpha1.<GROUP_NAME>".
func checkGroupName(g string) error {
	if g == "" {
		return fmt.Errorf("GROUP_NAME is required: set it to the groupName used in your issuers, for example contabo.acme.codejourney.io")
	}
	if len(g) > 253 || !strings.Contains(g, ".") {
		return fmt.Errorf("GROUP_NAME %q must be a domain-style name such as contabo.acme.codejourney.io", g)
	}
	for _, label := range strings.Split(g, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("GROUP_NAME %q is not a valid DNS name", g)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("GROUP_NAME %q may only use lower-case letters, digits, dots and dashes", g)
			}
		}
	}
	return nil
}
