package ca

import (
	"encoding/base64"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// secretType is kubernetes.io/tls on both Secrets: the API server then
	// validates that both keys are present, turning a truncated CA Secret
	// into a loud apply-time rejection instead of a CUBE-CA-002 discovered
	// on the next bootstrap.
	secretType = "kubernetes.io/tls"

	certDataKey = "tls.crt"
	keyDataKey  = "tls.key"
)

// SecretPlacement is where and under what names the CA and leaf Secrets
// land — the gateway domain's exported platform facts
// (docs/domains/gateway.md), injected into this domain at the CLI edge
// exactly like the namespace. This domain never derives placement or
// naming; the expected values are `cube-idp-ca` and `gateway-tls` per
// the gateway contract.
type SecretPlacement struct {
	// Namespace is the gateway namespace the Secrets are applied into.
	Namespace string
	// CASecret is the name of the Secret holding the cube's CA
	// certificate and private key.
	CASecret string
	// LeafSecret is the name of the Secret holding the gateway's
	// wildcard serving certificate and key.
	LeafSecret string
}

// SecretObjects returns the CA and leaf Secrets in apply order (CA
// first). Namespace and names are injected via p — gateway-owned
// platform facts this domain never derives. The objects are the inert
// prerequisite unit bootstrap SSA-applies: Secrets carry no status, so
// apply success is the unit's readiness.
func SecretObjects(p SecretPlacement, r EnsureResult) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		secretObject(p.Namespace, p.CASecret, r.CA),
		secretObject(p.Namespace, p.LeafSecret, r.Leaf),
	}
}

// MaterialFromSecret decodes CA material from a Secret the edge read
// from the cluster. Missing, empty, or non-base64 tls.crt/tls.key is
// CUBE-CA-002 — the same code the ensure path raises, because it is the
// same failure: existing material that cannot be used.
func MaterialFromSecret(obj *unstructured.Unstructured) (Material, error) {
	data, found, err := unstructured.NestedStringMap(obj.Object, "data")
	if err != nil {
		return Material{}, newUnusableSecretError(obj, "data is not a map of strings", err)
	}
	if !found {
		return Material{}, newUnusableSecretError(obj, "secret carries no data", nil)
	}
	certPEM, err := decodeSecretValue(obj, data, certDataKey)
	if err != nil {
		return Material{}, err
	}
	keyPEM, err := decodeSecretValue(obj, data, keyDataKey)
	if err != nil {
		return Material{}, err
	}
	return Material{CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

// secretObject builds one kubernetes.io/tls Secret. The data values are
// base64-encoded here rather than handed to stringData so that the write
// and read paths are one symmetric codec.
func secretObject(namespace, name string, m Material) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetAPIVersion("v1")
	o.SetKind("Secret")
	o.SetNamespace(namespace)
	o.SetName(name)
	_ = unstructured.SetNestedField(o.Object, secretType, "type")
	_ = unstructured.SetNestedField(o.Object, map[string]any{
		certDataKey: base64.StdEncoding.EncodeToString(m.CertPEM),
		keyDataKey:  base64.StdEncoding.EncodeToString(m.KeyPEM),
	}, "data")
	return o
}

// decodeSecretValue base64-decodes one required Secret data value.
func decodeSecretValue(obj *unstructured.Unstructured, data map[string]string, key string) ([]byte, error) {
	v, ok := data[key]
	if !ok || v == "" {
		return nil, newUnusableSecretError(obj, fmt.Sprintf("%s is missing or empty", key), nil)
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, newUnusableSecretError(obj, fmt.Sprintf("%s is not valid base64", key), err)
	}
	return raw, nil
}
