package solver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hayone1/cert-manager-webhook-contabo/internal/contabo"
)

// credentialSource says where a set of credentials came from, for error
// messages. It never holds secret values.
type credentialSource string

const ambientSource credentialSource = "the webhook's own environment (ambient credentials)"

// loadCredentials finds the Contabo credentials for one challenge.
//
// A Secret named in the config always wins. Without one, the webhook's own
// credentials (environment variables) are used, but only when cert-manager
// allows ambient credentials for this issuer: by default ClusterIssuers may
// use them and namespaced Issuers may not, so a namespace owner cannot use
// the cluster admin's Contabo account.
func loadCredentials(ctx context.Context, kube kubernetes.Interface, namespace string, cfg Config,
	allowAmbient bool, ambient *contabo.Credentials) (contabo.Credentials, credentialSource, error) {
	if cfg.SecretName != "" {
		return credentialsFromSecret(ctx, kube, namespace, cfg.SecretName, cfg.SecretKeys.withDefaults())
	}
	switch {
	case ambient != nil && allowAmbient:
		return *ambient, ambientSource, nil
	case ambient != nil:
		return contabo.Credentials{}, "", fmt.Errorf("the webhook config has no secretName, and cert-manager does not allow " +
			"ambient credentials for this issuer (namespaced Issuers may not use them by default). " +
			"Set secretName to a Secret in the Issuer's namespace")
	default:
		return contabo.Credentials{}, "", fmt.Errorf("the webhook config has no secretName. " +
			"Set config.secretName to the Secret that holds client-id, client-secret, api-user and api-password")
	}
}

func credentialsFromSecret(ctx context.Context, kube kubernetes.Interface, namespace, name string,
	keys SecretKeys) (contabo.Credentials, credentialSource, error) {
	src := credentialSource(fmt.Sprintf("Secret %s/%s", namespace, name))
	if kube == nil {
		return contabo.Credentials{}, src, fmt.Errorf("webhook is not initialized yet: no Kubernetes client")
	}
	if namespace == "" {
		return contabo.Credentials{}, src, fmt.Errorf("cannot read Secret %q: cert-manager sent no namespace", name)
	}
	secret, err := kube.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return contabo.Credentials{}, src, fmt.Errorf("%s not found. For a ClusterIssuer the Secret must be in "+
			"cert-manager's namespace; for an Issuer it must be in the Issuer's namespace", src)
	case apierrors.IsForbidden(err):
		return contabo.Credentials{}, src, fmt.Errorf("the webhook is not allowed to read %s. "+
			"Add the Secret name to the Helm value secretNames (Secrets in cert-manager's namespace) "+
			"or extraSecretAccess (Secrets in other namespaces)", src)
	case err != nil:
		return contabo.Credentials{}, src, fmt.Errorf("read %s: %w", src, err)
	}

	values := map[string]string{}
	var missing []string
	for _, k := range []string{keys.ClientID, keys.ClientSecret, keys.APIUser, keys.APIPassword} {
		v, ok := secret.Data[k]
		if !ok {
			if s, ok2 := secret.StringData[k]; ok2 { // only set on objects that were never stored
				v, ok = []byte(s), true
			}
		}
		// Values written with "echo" often end in a line break. IDs never
		// contain spaces; secrets and passwords keep inner and edge spaces
		// but lose line breaks at the ends.
		s := strings.Trim(string(v), "\r\n")
		if k == keys.ClientID || k == keys.APIUser {
			s = strings.TrimSpace(s)
		}
		if !ok || s == "" {
			missing = append(missing, k)
			continue
		}
		values[k] = s
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return contabo.Credentials{}, src, fmt.Errorf("%s is missing these keys or they are empty: %s",
			src, strings.Join(missing, ", "))
	}
	return contabo.Credentials{
		ClientID:     values[keys.ClientID],
		ClientSecret: values[keys.ClientSecret],
		APIUser:      values[keys.APIUser],
		APIPassword:  values[keys.APIPassword],
	}, src, nil
}
