package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// secretGVR addresses core Secrets. The GVR is written out rather than
// resolved through the REST mapper: v1/Secret is a core kind that needs no
// discovery round-trip, and this read runs before the mapper is otherwise
// touched. It is built per call rather than held in a package-level value,
// which would be mutable state outside main however it is meant.
func secretGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
}

// secretReader reads one Secret by name from the gateway namespace. It is the
// edge's consumer-side seam onto the live cluster for the one read the CA
// handoff needs — a function, like the pack contract's document resolver, so a
// test supplies a value rather than a client.
type secretReader func(ctx context.Context, name string) (*unstructured.Unstructured, error)

// gatewaySecretReader binds the live dynamic client to the gateway namespace.
func gatewaySecretReader(dyn dynamic.Interface) secretReader {
	return func(ctx context.Context, name string) (*unstructured.Unstructured, error) {
		return dyn.Resource(secretGVR()).Namespace(gateway.Namespace).Get(ctx, name, metav1.GetOptions{})
	}
}

// caInputs are the mint-if-absent inputs: who provides the CA, what it is
// minted for, and the clock and entropy the result is a function of.
type caInputs struct {
	// provider is the resolved spec.ca.provider.
	provider string
	// cubeName is metadata.name of the cube; it forms the CA's marker CN.
	cubeName string
	// domain is the cube's base domain; the leaf covers *.<domain>.
	domain string
	// now anchors both certificates' validity window.
	now time.Time
	// rand is the entropy source; production passes crypto/rand.Reader.
	rand io.Reader
}

// ensureCA returns the effective CA material for this bootstrap: the material
// already in the cluster's CA Secret when it holds usable material, a freshly
// minted CA when the Secret is absent. It is the edge's first cluster read
// outside a domain operation — internal/ca is pure and internal/bootstrap
// gains no read — and it never re-mints over material it cannot read
// (CUBE-CA-002 passes through).
//
// The provider switch is the edge's, exactly as the engine factory's is:
// driver selection is composition, and the domain owns the code it raises.
func ensureCA(ctx context.Context, read secretReader, in caInputs) (ca.EnsureResult, error) {
	if in.provider != ca.ProviderCube {
		return ca.EnsureResult{}, ca.NewUnsupportedProviderError(in.provider)
	}
	existing, err := readCAMaterial(ctx, read)
	if err != nil {
		return ca.EnsureResult{}, err
	}
	return ca.Ensure(ca.EnsureRequest{
		MintRequest: ca.MintRequest{
			CubeName: in.cubeName,
			Domain:   in.domain,
			Now:      in.now,
			Rand:     in.rand,
		},
		ExistingCA: existing,
	})
}

// readCAMaterial reads the cube's CA Secret and decodes its material. A
// NotFound is absence, not a failure — nil material means "mint". On a first
// bootstrap the gateway namespace does not exist yet either, and that read is
// NotFound too, which is the same answer. Every other API failure is the
// edge's own, wrapped uncoded: the CLI has originated no code catalog, and
// this is not the ca domain's failure to claim.
func readCAMaterial(ctx context.Context, read secretReader) (*ca.Material, error) {
	obj, err := read(ctx, gateway.CASecretName)
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read the CA secret %s/%s: %w",
			gateway.Namespace, gateway.CASecretName, err)
	}
	material, err := ca.MaterialFromSecret(obj)
	if err != nil {
		return nil, err
	}
	return &material, nil
}

// ensureCAMaterial runs the CA handoff for one bootstrap, gated on the
// resolved list: a list that drops ca-secrets neither reads, mints, nor syncs
// anything, and the zero result it returns reaches no unit.
//
// The read gets its own short budget derived from the command's context,
// never the install budget: --timeout is bootstrap's readiness budget, and an
// edge round-trip inheriting a nearly-spent one would fail for lack of time
// rather than for a fault.
func ensureCAMaterial(ctx context.Context, dyn dynamic.Interface, cfg *v1alpha1.Config, domain string, deps bootstrapDeps) (ca.EnsureResult, error) {
	if !hasPrerequisite(cfg.Spec.Prerequisites, v1alpha1.PrerequisiteCASecrets) {
		return ca.EnsureResult{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, edgeIOTimeout)
	defer cancel()
	return ensureCA(ctx, gatewaySecretReader(dyn), caInputs{
		provider: caProvider(cfg),
		cubeName: cfg.Name,
		domain:   domain,
		now:      deps.now(),
		rand:     deps.rand,
	})
}

// caProvider resolves who provides the cube's CA. An absent spec.ca means the
// cube provider — api's Default() only fills a sub-struct the user wrote, so
// the edge derives the default, the gatewayDomain shape applied to the same
// absence.
func caProvider(cfg *v1alpha1.Config) string {
	if cfg.Spec.CA == nil {
		return ca.ProviderCube
	}
	return string(cfg.Spec.CA.Provider)
}
