package pack

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// stubResolver serves documents from a map and records every reference it was
// asked for, so a test can assert not only what was resolved but that nothing
// was. An unknown reference answers fs.ErrNotExist, which stands in for the
// resolver's own failure and stays recognisable through wrapping.
type stubResolver struct {
	docs  map[string]string
	calls []string
}

func (r *stubResolver) resolve(_ context.Context, reference string) ([]byte, error) {
	r.calls = append(r.calls, reference)
	doc, ok := r.docs[reference]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(doc), nil
}

// wantCoded asserts the error carries exactly the expected coded identity. It
// is the in-package twin of the helper the external tests use, because the
// values pipeline and the render seam are unexported.
func wantCoded(t *testing.T, err error, want cubeerr.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want %s", want)
	}
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded, want %s", err, want)
	}
	if coded.Code != want {
		t.Errorf("error code = %s, want %s", coded.Code, want)
	}
	if coded.Remediation == "" {
		t.Errorf("error %s carries no remediation", coded.Code)
	}
}

// instanceSpec builds the two values fields of a setup entry. inline is the
// JSON api/ keeps an inline value as; empty means no inline values at all.
func instanceSpec(valuesRef, inline string) v1alpha1.PackSpec {
	spec := v1alpha1.PackSpec{ValuesRef: valuesRef}
	if inline != "" {
		spec.Values = &runtime.RawExtension{Raw: []byte(inline)}
	}
	return spec
}
