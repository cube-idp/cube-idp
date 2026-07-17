// vendor_auth_test.go pins the docker-credential side of image vendoring:
// pullImageTar must authenticate with `docker login` credentials when the
// registry demands them (same contract pullauth_test.go pins for pack
// pulls), not pull anonymously and surface the 401 as CUBE-2xxx.
package bundle

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/random"

	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
)

func TestPullImageTarUsesDockerCredentials(t *testing.T) {
	host := ocitest.LocalRegistryWithBasicAuth(t, "cube", "s3cret")
	ocitest.SetDockerAuth(t, host, "cube", "s3cret")
	imgRef := host + "/images/demo:v1"

	// Seed the authed registry directly with go-containerregistry (TEST-ONLY
	// dependency, Owner Decisions #2) — pushTestImage's crane path plus basic
	// auth. No platform mutation needed: pullImageTar is called with a nil
	// platform, so oras performs no platform match.
	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := crane.Push(img, imgRef, crane.Insecure,
		crane.WithAuth(&authn.Basic{Username: "cube", Password: "s3cret"})); err != nil {
		t.Fatalf("seeding basic-auth registry: %v", err)
	}

	layout := filepath.Join(t.TempDir(), "layout")
	if err := pullImageTar(context.Background(), imgRef, layout, nil); err != nil {
		t.Fatalf("pullImageTar through basic-auth registry: %v", err)
	}
}
