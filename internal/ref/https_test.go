package ref

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPSSuccess covers the core of the backend: 200 with a body becomes
// a ResolvedFile carrying exactly those bytes and a pin over them. It runs
// against a real httptest.Server with the real client that server
// publishes for its own certificate — nothing about the HTTP layer is
// mocked.
func TestHTTPSSuccess(t *testing.T) {
	const body = `replicas: 3
image: registry.example.com/demo:0.1.0
`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	url := srv.URL + "/values.yaml"
	p, err := parse(url)
	if err != nil {
		t.Fatalf("parse(%q) error = %v, want nil", url, err)
	}

	got, err := fetchHTTPS(t.Context(), p, srv.Client())
	if err != nil {
		t.Fatalf("fetchHTTPS(%q) error = %v, want nil", url, err)
	}
	if string(got.Bytes()) != body {
		t.Errorf("fetchHTTPS(%q).Bytes() = %q, want %q", url, got.Bytes(), body)
	}

	pin := got.Pin()
	if pin.Ref != url {
		t.Errorf("fetchHTTPS(%q).Pin().Ref = %q, want %q", url, pin.Ref, url)
	}
	if pin.Source != url {
		t.Errorf("fetchHTTPS(%q).Pin().Source = %q, want %q", url, pin.Source, url)
	}
	if want := fileDigest([]byte(body)); pin.Digest != want {
		t.Errorf("fetchHTTPS(%q).Pin().Digest = %q, want %q", url, pin.Digest, want)
	}
}

// TestHTTPSErrorPaths covers what the https backend does when the server
// answers with something other than a document. Every case runs against a
// real httptest.Server; the HTTP layer is never mocked.
func TestHTTPSErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "no such pack", http.StatusNotFound)
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
		},
		{
			name: "redirect to nowhere resolvable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(tt.handler)
			t.Cleanup(srv.Close)

			p, err := parse(srv.URL + "/values.yaml")
			if err != nil {
				t.Fatalf("parse(%q) error = %v, want nil", srv.URL, err)
			}
			_, err = fetchHTTPS(t.Context(), p, srv.Client())
			requireCode(t, err, CodeFetchFailed)
		})
	}
}

// TestHTTPSWiredThroughPublicEntryPoint exercises httpsFile itself — the
// function backendFor wires in, which builds its own client — rather than
// fetchHTTPS. A closed server is the one failure a default client can be
// driven into from the public API, since it cannot be taught to trust an
// httptest certificate; the success path is covered through fetchHTTPS.
func TestHTTPSWiredThroughPublicEntryPoint(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/values.yaml"
	srv.Close()

	_, err := ResolveFile(t.Context(), url)
	requireCode(t, err, CodeFetchFailed)
}

// TestHTTPSUnreachableServer covers the transport failing outright.
func TestHTTPSUnreachableServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := srv.Client()
	url := srv.URL + "/values.yaml"
	srv.Close()

	p, err := parse(url)
	if err != nil {
		t.Fatalf("parse(%q) error = %v, want nil", url, err)
	}
	if _, err := fetchHTTPS(t.Context(), p, client); err == nil {
		t.Fatal("fetchHTTPS against a closed server = nil error, want a fetch failure")
	} else {
		requireCode(t, err, CodeFetchFailed)
	}
}
