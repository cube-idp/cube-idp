package ca

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The ca domain owns the CUBE-CA-* code range. Codes are declared here
// and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Only the codes this package raises are declared:
// CUBE-CA-003 (ledger) and CUBE-CA-004 (trust store) stay reserved by the
// contract and land beside the content that raises them. Constructors
// are unexported except NewUnsupportedProviderError, which the CLI edge's
// provider switch raises.
const (
	// CodeMint reports a failure to mint the CA or its leaf (wrapped
	// stdlib cause).
	CodeMint cubeerr.Code = "CUBE-CA-001"
	// CodeUnusableMaterial reports existing CA Secret material that is
	// unparseable or incomplete.
	CodeUnusableMaterial cubeerr.Code = "CUBE-CA-002"
	// CodeUnsupportedProvider reports a spec.ca.provider no
	// implementation provides. It extends the gate's initial catalog
	// under the normal per-domain rule (docs/domains/ca.md).
	CodeUnsupportedProvider cubeerr.Code = "CUBE-CA-005"
)

// What a mint failure names, so the summary says which certificate.
const (
	subjectCA   = "the cube CA"
	subjectLeaf = "the gateway certificate"
)

// remediateDelete is the shared tail of both CUBE-CA-002 remediations:
// deleting the Secret re-mints, and that breaks every client that
// installed the old CA.
const remediateDelete = "to re-mint the cube CA — every client that trusted the old CA must re-run `cube-idp trust install`"

// newMintError reports a failed mint. A stdlib crypto failure is not
// user-actionable, so the remediation asks for a report.
func newMintError(what string, cause error) error {
	return cubeerr.Wrap(CodeMint,
		fmt.Sprintf("cannot mint %s", what),
		"this is an internal error; please report it",
		cause)
}

// newUnusableMaterialError reports existing CA material the domain
// cannot use. It is the Ensure path, which carries neither namespace nor
// name — the Secret is described, not named; the richer
// newUnusableSecretError path is what users actually hit.
func newUnusableMaterialError(detail string, cause error) error {
	return cubeerr.Wrap(CodeUnusableMaterial,
		fmt.Sprintf("existing CA material is unusable: %s", detail),
		fmt.Sprintf("delete the cube CA Secret in the gateway namespace %s", remediateDelete),
		cause)
}

// newUnusableSecretError reports an unusable CA Secret the edge read.
// Holding the object, it names both namespace and name — the richer of
// the two CUBE-CA-002 messages, and the one a user actually hits.
func newUnusableSecretError(obj *unstructured.Unstructured, detail string, cause error) error {
	namespace, name := obj.GetNamespace(), obj.GetName()
	return cubeerr.Wrap(CodeUnusableMaterial,
		fmt.Sprintf("existing CA secret %s/%s is unusable: %s", namespace, name, detail),
		fmt.Sprintf("delete secret %s in namespace %s %s", name, namespace, remediateDelete),
		cause)
}

// NewUnsupportedProviderError reports a spec.ca.provider no
// implementation provides.
func NewUnsupportedProviderError(provider string) error {
	return cubeerr.Wrap(CodeUnsupportedProvider,
		fmt.Sprintf("no implementation for CA provider %q", provider),
		fmt.Sprintf("use a supported spec.ca.provider (%s)", ProviderCube), nil)
}
