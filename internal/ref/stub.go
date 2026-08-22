package ref

import "context"

// The grammar recognizes these three backends so a reference written for
// one is answered by name instead of by "unsupported scheme" — a user who
// writes a git reference has made no syntax error, they have reached a
// backend this build does not carry. Each keeps its own code, so the error
// says which backend is missing and where it lands. No SDK is imported
// here: go-git/v5, oras-go/v2 and the AWS SDK are each owed their own
// dependency gate (docs/ARCHITECTURE.md §8).
const (
	gitLandsIn = "its own milestone, behind the go-git/v5 dependency gate"
	ociLandsIn = "M10, behind the oras-go/v2 dependency gate"
	s3LandsIn  = "its own milestone, behind the AWS SDK dependency gate"
)

func gitTree(_ context.Context, p parsed) (ResolvedTree, error) {
	return ResolvedTree{}, newNotImplementedError(CodeGitNotImplemented, "git", p.ref, gitLandsIn)
}

func gitFile(_ context.Context, p parsed) (ResolvedFile, error) {
	return ResolvedFile{}, newNotImplementedError(CodeGitNotImplemented, "git", p.ref, gitLandsIn)
}

func ociTree(_ context.Context, p parsed) (ResolvedTree, error) {
	return ResolvedTree{}, newNotImplementedError(CodeOCINotImplemented, "oci", p.ref, ociLandsIn)
}

func ociFile(_ context.Context, p parsed) (ResolvedFile, error) {
	return ResolvedFile{}, newNotImplementedError(CodeOCINotImplemented, "oci", p.ref, ociLandsIn)
}

func s3Tree(_ context.Context, p parsed) (ResolvedTree, error) {
	return ResolvedTree{}, newNotImplementedError(CodeS3NotImplemented, "s3", p.ref, s3LandsIn)
}

func s3File(_ context.Context, p parsed) (ResolvedFile, error) {
	return ResolvedFile{}, newNotImplementedError(CodeS3NotImplemented, "s3", p.ref, s3LandsIn)
}
