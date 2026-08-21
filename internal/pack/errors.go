package pack

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The pack domain owns the CUBE-PKG-* code range. Codes are declared here and
// nowhere else; the cross-domain tag registry lives in docs/ARCHITECTURE.md and
// the catalog they implement is docs/domains/pack.md. Only the codes this build
// can actually raise are declared — later chunks add their own rows rather than
// reserving numbers up front. Constructors stay unexported unless the CLI edge
// itself must raise the error.
const (
	// CodeSourceUnreadable reports a pack source with no readable pack.cue
	// at its root.
	CodeSourceUnreadable cubeerr.Code = "CUBE-PKG-001"
	// CodeMetadataCompile reports a pack.cue that does not compile as CUE.
	CodeMetadataCompile cubeerr.Code = "CUBE-PKG-002"
	// CodeMetadataSchema reports a pack.cue that compiles but does not
	// satisfy the pack schema.
	CodeMetadataSchema cubeerr.Code = "CUBE-PKG-003"
	// CodePayloadMismatch reports a payload that does not match the type the
	// pack declares.
	CodePayloadMismatch cubeerr.Code = "CUBE-PKG-004"
)

func newSourceUnreadableError(ref string, cause error) error {
	return cubeerr.Wrap(CodeSourceUnreadable,
		fmt.Sprintf("no readable %s at pack source %q", MetadataFile, ref),
		fmt.Sprintf("point the reference at a directory containing %s", MetadataFile), cause)
}

// NewRefUnsupportedError reports a pack reference this build cannot resolve.
// It is exported because reference resolution lives at the CLI edge until the
// reference leaf lands, and that edge must raise a pack-coded error rather
// than invent one of its own.
func NewRefUnsupportedError(ref, scheme string) error {
	return cubeerr.Wrap(CodeSourceUnreadable,
		fmt.Sprintf("cannot resolve pack reference %q: scheme %q is not supported in this build", ref, scheme),
		"this build resolves local paths only (./dir, /abs/dir, file:///abs/dir); remote schemes land with the reference resolver", nil)
}

func newMetadataCompileError(cause error) error {
	return cubeerr.Wrap(CodeMetadataCompile,
		fmt.Sprintf("%s does not compile as CUE", MetadataFile),
		fmt.Sprintf("fix the CUE syntax reported below in %s", MetadataFile), cause)
}

func newMetadataSchemaError(cause error) error {
	return cubeerr.Wrap(CodeMetadataSchema,
		fmt.Sprintf("%s does not satisfy the pack schema", MetadataFile),
		"a pack declares name, version, and type (raw|helm|kustomize), plus optional namespace and category — nothing else", cause)
}

func newPayloadMismatchError(t Type, want string) error {
	return cubeerr.Wrap(CodePayloadMismatch,
		fmt.Sprintf("pack declares type %q but its payload has no %s", t, want),
		fmt.Sprintf("add %s, or declare the type the payload actually is", want), nil)
}
