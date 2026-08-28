package ca

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The ca domain owns the CUBE-CA-* code range. Codes are declared here
// and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors are unexported except where the CLI
// edge raises the code itself — the provider switch
// (NewUnsupportedProviderError), the ledger read
// (NewLedgerUnreadableError), and the trust verbs' preconditions, since
// the domain never touches files and never chooses the store.
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
	// CodeTrustStore reports a failed trust-store operation: an OS tool
	// that is missing, unusable, or exited non-zero; a missing operator
	// artifact; an OS with no driver; or a fingerprint/marker mismatch
	// that refuses a removal.
	CodeTrustStore cubeerr.Code = "CUBE-CA-004"
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

// newTrustStoreError is the shared CUBE-CA-004 constructor. Every trust
// failure carries a remediation the user can act on without cube-idp:
// the OS trust stores are the one place the tool cannot retry on the
// user's behalf, so "install it by hand, like this" is the answer.
func newTrustStoreError(summary, remediation string, cause error) error {
	return cubeerr.Wrap(CodeTrustStore, summary, remediation, cause)
}

// newRemovalRefusedError refuses a removal whose candidate certificate
// failed either half of the identity check. Refusal is the whole point
// of the check: cube-idp deletes nothing it has not identified as this
// cube's own CA.
func newRemovalRefusedError(cube string, cause error) error {
	return newTrustStoreError(
		fmt.Sprintf("refusing to remove a certificate that is not cube %q's CA", cube),
		fmt.Sprintf("check the trust ledger (~/%s/%s); remove the certificate by hand if it is genuinely yours",
			DirName, LedgerFileName), cause)
}

// NewMissingArtifactError reports a `trust install` for a cube whose CA
// certificate has never been emitted. The verb consumes the artifact and
// never mints one: a second source of truth for the CA is exactly what
// the reuse contract exists to prevent.
func NewMissingArtifactError(cube, path string, cause error) error {
	return newTrustStoreError(
		fmt.Sprintf("cannot install the CA for cube %q: no certificate at %s", cube, path),
		`run "cube-idp bootstrap" to emit it`, cause)
}

// NewUnusableArtifactError reports an emitted CA certificate that does
// not parse. Bootstrap rewrites a divergent artifact, so re-running it
// is the remedy.
func NewUnusableArtifactError(cube, path string, cause error) error {
	return newTrustStoreError(
		fmt.Sprintf("cannot install the CA for cube %q: the certificate at %s does not parse", cube, path),
		`re-run "cube-idp bootstrap" to re-emit it`, cause)
}

// NewArtifactReadError reports a CA certificate artifact that could not
// be read for a reason other than its absence — a permission error, for
// instance. An absent artifact is a different, verb-specific condition
// (NewMissingArtifactError or an absent-file pass-through); this code is
// for the file being there but unreadable.
func NewArtifactReadError(cube, path string, cause error) error {
	return newTrustStoreError(
		fmt.Sprintf("cannot read the CA certificate at %s for cube %q", path, cube),
		`check the file's permissions, or run "cube-idp bootstrap" to re-emit it`, cause)
}

// NewUnsupportedStoreError reports an operating system with no
// user-scope trust store driver. It is never a silent no-op: the
// artifact exists either way, so the remediation hands over its path.
func NewUnsupportedStoreError(goos, certPath string) error {
	return newTrustStoreError(
		fmt.Sprintf("no user-scope trust store driver for %s", goos),
		fmt.Sprintf("cube-idp emitted the CA at %s — install it with your platform's certificate manager",
			certPath), nil)
}

// NewStoreMismatchError refuses a removal the ledger records against a
// different store than the running machine's. The ledger's store field
// exists precisely to make that decidable, and acting anyway would mean
// searching a store the certificate was never put into.
func NewStoreMismatchError(cube, recorded, current string) error {
	return newTrustStoreError(
		fmt.Sprintf("cannot remove cube %q's CA: the ledger records it in the %s store, but this machine's is %s",
			cube, recorded, current),
		fmt.Sprintf("run `cube-idp trust remove %s` on the machine that installed it, or remove the "+
			"certificate by hand and delete the entry from ~/%s/%s", cube, DirName, LedgerFileName), nil)
}
