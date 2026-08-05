package kube

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// kubeconfigBytes serializes a minimal kubeconfig with one cluster/user
// pair and the given contexts, all pointing at server.
func kubeconfigBytes(t *testing.T, server, currentContext string, contextNames ...string) []byte {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["cluster"] = &clientcmdapi.Cluster{Server: server}
	cfg.AuthInfos["user"] = &clientcmdapi.AuthInfo{}
	for _, name := range contextNames {
		cfg.Contexts[name] = &clientcmdapi.Context{Cluster: "cluster", AuthInfo: "user"}
	}
	cfg.CurrentContext = currentContext
	raw, err := clientcmd.Write(*cfg)
	if err != nil {
		t.Fatalf("clientcmd.Write() failed: %v", err)
	}
	return raw
}

func TestNew(t *testing.T) {
	valid := kubeconfigBytes(t, "https://example.invalid:6443", "main", "main", "other")
	noCurrent := kubeconfigBytes(t, "https://example.invalid:6443", "", "main")

	brokenRef := clientcmdapi.NewConfig()
	brokenRef.AuthInfos["user"] = &clientcmdapi.AuthInfo{}
	brokenRef.Contexts["dangling"] = &clientcmdapi.Context{Cluster: "missing", AuthInfo: "user"}
	brokenRefBytes, err := clientcmd.Write(*brokenRef)
	if err != nil {
		t.Fatalf("clientcmd.Write() failed: %v", err)
	}

	cases := []struct {
		name        string
		kubeconfig  []byte
		contextName string
		wantCode    cubeerr.Code // "" = success
	}{
		{name: "explicit context", kubeconfig: valid, contextName: "other"},
		{name: "empty context name uses current-context", kubeconfig: valid},
		{name: "unparseable bytes", kubeconfig: []byte("not: [valid"), wantCode: CodeInvalidKubeconfig},
		{name: "context not found", kubeconfig: valid, contextName: "nope", wantCode: CodeContextNotFound},
		{name: "no current-context and empty name", kubeconfig: noCurrent, wantCode: CodeContextNotFound},
		{name: "context references missing cluster", kubeconfig: brokenRefBytes,
			contextName: "dangling", wantCode: CodeConstructionFailed},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.kubeconfig, tt.contextName)
			if tt.wantCode != "" {
				var coded *cubeerr.Coded
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("New(_, %q) error = %v, want code %s", tt.contextName, err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(_, %q) failed: %v", tt.contextName, err)
			}
			if client.RESTConfig() == nil || client.Discovery() == nil ||
				client.RESTMapper() == nil || client.Dynamic() == nil {
				t.Errorf("New(_, %q) returned a client with nil accessors: %+v", tt.contextName, client)
			}
		})
	}
}

func TestPing(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantCode cubeerr.Code // "" = success
	}{
		{name: "ready API server", status: http.StatusOK},
		{name: "unready API server", status: http.StatusInternalServerError, wantCode: CodeUnreachable},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)
			client, err := New(kubeconfigBytes(t, server.URL, "main", "main"), "")
			if err != nil {
				t.Fatalf("New() failed: %v", err)
			}
			err = client.Ping(t.Context())
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Ping() = %v, want nil", err)
				}
				return
			}
			var coded *cubeerr.Coded
			if !errors.As(err, &coded) || coded.Code != tt.wantCode {
				t.Fatalf("Ping() error = %v, want code %s", err, tt.wantCode)
			}
		})
	}
}

func TestPingUnreachableServer(t *testing.T) {
	server := httptest.NewServer(http.NewServeMux())
	client, err := New(kubeconfigBytes(t, server.URL, "main", "main"), "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	server.Close()

	var coded *cubeerr.Coded
	if err := client.Ping(t.Context()); !errors.As(err, &coded) || coded.Code != CodeUnreachable {
		t.Fatalf("Ping() against a closed server = %v, want code %s", err, CodeUnreachable)
	}
}
