package ca

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The ca domain owns the CUBE-CA-* code range. Codes are declared here
// and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Only the codes this package raises are declared:
// CUBE-CA-004 (trust store) stays reserved by the contract and lands
// beside the content that raises it. Constructors are unexported except
// where the CLI edge raises the code itself — the provider switch
// (NewUnsupportedProviderError) and the ledger read
// (NewLedgerUnreadableError), since the domain never touches files.
const (
	// CodeMint reports a failure to mint the CA or its leaf (wrapped
	// stdlib cause).
	CodeMint cubeerr.Code = "CUBE-CA-001"
	// CodeUnusableMaterial reports existing CA Secret material that is
	// unparseable or incomplete.
	CodeUnusableMaterial cubeerr.Code = "CUBE-CA-002"
	// CodeLedger reports a trust ledger that cannot be read or does not
	// parse.
	CodeLedger cubeerr.Code = "CUBE-CA-003"
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

// remediateLedger is the shared CUBE-CA-003 remediation. Deleting the
// ledger is safe in the only sense that matters: it is a record of
// installations, so losing it changes nothing in any trust store.
const remediateLedger = "fix or delete the trust ledger (~/.cube-idp/trust.yaml); " +
	"deleting it loses the record of installed CAs but removes nothing from your trust store"

// newLedgerError reports a trust ledger the domain cannot decode. The
// detail completes "the trust ledger <detail>".
func newLedgerError(detail string, cause error) error {
	return cubeerr.Wrap(CodeLedger,
		fmt.Sprintf("the trust ledger %s", detail),
		remediateLedger, cause)
}

// NewLedgerUnreadableError reports a trust ledger the CLI edge could not
// read. The domain never touches the filesystem, so the "unreadable"
// half of CUBE-CA-003 is raised at the edge; an absent file is not one
// of these — it is simply an empty ledger.
func NewLedgerUnreadableError(path string, cause error) error {
	return cubeerr.Wrap(CodeLedger,
		fmt.Sprintf("cannot read the trust ledger %s", path),
		remediateLedger, cause)
}

// NewUnsupportedProviderError reports a spec.ca.provider no
// implementation provides.
func NewUnsupportedProviderError(provider string) error {
	return cubeerr.Wrap(CodeUnsupportedProvider,
		fmt.Sprintf("no implementation for CA provider %q", provider),
		fmt.Sprintf("use a supported spec.ca.provider (%s)", ProviderCube), nil)
}
