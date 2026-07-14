package bundle

import "sort"

// ImageLoad pairs an image reference with the bundle-local tar path holding
// it (a single-image OCI-layout tar, Task 6). The ImageLoader providers
// (kindp, k3dp) consume these to node-load images with no registry pull.
type ImageLoad struct {
	Ref string // original image reference (the Manifest.Images key)
	Tar string // absolute path to that image's tar inside the extraction root
}

// SortedImageLoads turns an ImageTars() map into ref/path pairs ordered by
// image reference. Go map iteration is unordered; sorting here makes both the
// load sequence and its progress output deterministic across runs — the
// property Task 13's bundle e2e and byte-stable output both rely on. The
// providers implementing cluster.ImageLoader call this so neither reimplements
// the ordering.
func SortedImageLoads(imageTars map[string]string) []ImageLoad {
	refs := make([]string, 0, len(imageTars))
	for ref := range imageTars {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := make([]ImageLoad, len(refs))
	for i, ref := range refs {
		out[i] = ImageLoad{Ref: ref, Tar: imageTars[ref]}
	}
	return out
}
