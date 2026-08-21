package pack

import (
	"fmt"
	"strings"

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
	// CodeManifestParse reports a manifest file that is not valid YAML.
	CodeManifestParse cubeerr.Code = "CUBE-PKG-005"
	// CodeKustomizeBuild reports a kustomization that failed to build.
	CodeKustomizeBuild cubeerr.Code = "CUBE-PKG-006"
	// CodeEmptyRender reports a pack that rendered no objects at all.
	CodeEmptyRender cubeerr.Code = "CUBE-PKG-007"
	// CodeNamespaceConflict reports an object whose own namespace contradicts
	// the namespace the pack forces every namespaced object into.
	CodeNamespaceConflict cubeerr.Code = "CUBE-PKG-008"
	// CodeValuesOnRawPack reports values supplied to a raw pack, which has no
	// values surface.
	CodeValuesOnRawPack cubeerr.Code = "CUBE-PKG-009"
	// CodeValuesRejected reports values that the pack's #Values definition
	// rejects: an undeclared field, a type mismatch, or a missing required
	// value.
	CodeValuesRejected cubeerr.Code = "CUBE-PKG-010"
	// CodeValuesNotFlat reports a kustomize pack whose validated values are not
	// a flat map of strings.
	CodeValuesNotFlat cubeerr.Code = "CUBE-PKG-011"
	// CodeSubstitutionMissing reports a ${VAR} reference in the built output
	// that no value resolves.
	CodeSubstitutionMissing cubeerr.Code = "CUBE-PKG-012"
	// CodeRenderTypeUnsupported reports a pack whose declared type this build
	// cannot render. The summary names the type and the milestone that
	// implements it.
	CodeRenderTypeUnsupported cubeerr.Code = "CUBE-PKG-020"
	// CodeRemoteRef reports a kustomize payload that references a remote
	// resource, which a hermetic renderer refuses to fetch.
	CodeRemoteRef cubeerr.Code = "CUBE-PKG-021"
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

func newFileUnreadableError(name string, cause error) error {
	return cubeerr.Wrap(CodeSourceUnreadable,
		fmt.Sprintf("cannot read %s from the pack source", name),
		"check the file's permissions and that the pack source is intact", cause)
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

func newManifestParseError(name string, cause error) error {
	return cubeerr.Wrap(CodeManifestParse,
		fmt.Sprintf("cannot parse manifest %s", name),
		fmt.Sprintf("fix the YAML at %s and re-run", name), cause)
}

func newEmptyRenderError(name string) error {
	return cubeerr.Wrap(CodeEmptyRender,
		fmt.Sprintf("pack %q rendered no objects", name),
		fmt.Sprintf("add at least one Kubernetes object under %s/", ManifestsDir), nil)
}

func newNamespaceConflictError(kind, name, got, want string) error {
	return cubeerr.Wrap(CodeNamespaceConflict,
		fmt.Sprintf("%s %q declares namespace %q, but the pack forces namespace %q", kind, name, got, want),
		fmt.Sprintf("drop the namespace from the manifest, set it to %q, or remove namespace from %s", want, MetadataFile), nil)
}

func newValuesOnRawPackError() error {
	return cubeerr.Wrap(CodeValuesOnRawPack,
		"values supplied to a raw pack",
		"raw packs have no values surface — drop the values, or declare type helm or kustomize", nil)
}

func newValuesRejectedError(cause error) error {
	return cubeerr.Wrap(CodeValuesRejected,
		"values rejected by the pack's #Values definition",
		fmt.Sprintf("supply only the fields #Values declares in %s, with their declared types", MetadataFile), cause)
}

func newKustomizeBuildError(cause error) error {
	return cubeerr.Wrap(CodeKustomizeBuild,
		"kustomize build failed",
		"fix the kustomization reported below; `kustomize build` on the pack directory reproduces it", cause)
}

func newValuesNotFlatError(name string, value any) error {
	return cubeerr.Wrap(CodeValuesNotFlat,
		fmt.Sprintf("value %q is %T, but kustomize values must be a flat map of strings", name, value),
		fmt.Sprintf("quote %s as a string, or move the structure into an overlay in the pack payload", name), nil)
}

func newSubstitutionError(names []string) error {
	return cubeerr.Wrap(CodeSubstitutionMissing,
		fmt.Sprintf("no value for ${%s} in the rendered output", strings.Join(names, "}, ${")),
		fmt.Sprintf("supply %s in values, or escape the reference as $${...} to keep it literal",
			strings.Join(names, ", ")), nil)
}

func newRemoteRefError(file, ref string) error {
	return cubeerr.Wrap(CodeRemoteRef,
		fmt.Sprintf("%s references the remote resource %q", file, ref),
		"a pack payload is self-contained — vendor the base into the pack; rendering never fetches over the network", nil)
}

func newRenderTypeUnsupportedError(t Type, milestone string) error {
	return cubeerr.Wrap(CodeRenderTypeUnsupported,
		fmt.Sprintf("rendering type %q is not implemented in this build", t),
		fmt.Sprintf("%s rendering lands in %s; use type raw until then", t, milestone), nil)
}
