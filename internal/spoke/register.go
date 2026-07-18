package spoke

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// BuildKubeconfig renders a self-contained kubeconfig for server with the
// bearer token and CA — the flux hub secret's `value` payload.
func BuildKubeconfig(clusterName, server string, caData []byte, token string) ([]byte, error) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[clusterName] = &clientcmdapi.Cluster{Server: server, CertificateAuthorityData: caData}
	cfg.AuthInfos[clusterName] = &clientcmdapi.AuthInfo{Token: token}
	cfg.Contexts[clusterName] = &clientcmdapi.Context{Cluster: clusterName, AuthInfo: clusterName}
	cfg.CurrentContext = clusterName
	return clientcmd.Write(*cfg)
}

// argocdClusterConfig is argocd's cluster-secret `config` JSON payload.
type argocdClusterConfig struct {
	BearerToken     string `json:"bearerToken"`
	TLSClientConfig struct {
		CAData []byte `json:"caData"`
	} `json:"tlsClientConfig"`
}

// HubSecrets returns the engine-native registration secret(s) for one
// spoke: argocd → argocd cluster secret in ns "argocd"; flux → kubeconfig
// secret (key "value") in ns "flux-system". Both named cube-idp-spoke-<name>.
func HubSecrets(engineType, spokeName, server string, cred *Credential) ([]*unstructured.Unstructured, error) {
	name := "cube-idp-spoke-" + spokeName
	switch engineType {
	case "argocd":
		cc := argocdClusterConfig{BearerToken: cred.Token}
		cc.TLSClientConfig.CAData = cred.CAData
		cj, err := json.Marshal(cc)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeSpokeRegisterFailed, "cannot encode argocd cluster config", "report this as a bug")
		}
		return []*unstructured.Unstructured{{Object: map[string]any{
			"apiVersion": "v1", "kind": "Secret",
			"metadata": map[string]any{
				"name": name, "namespace": "argocd",
				"labels": map[string]any{"argocd.argoproj.io/secret-type": "cluster"},
			},
			"type": "Opaque",
			"stringData": map[string]any{
				"name":   spokeName,
				"server": server,
				"config": string(cj),
			},
		}}}, nil
	case "flux":
		kc, err := BuildKubeconfig(spokeName, server, cred.CAData, cred.Token)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeSpokeRegisterFailed, "cannot render spoke kubeconfig", "report this as a bug")
		}
		return []*unstructured.Unstructured{{Object: map[string]any{
			"apiVersion": "v1", "kind": "Secret",
			"metadata":   map[string]any{"name": name, "namespace": "flux-system"},
			"type":       "Opaque",
			"stringData": map[string]any{"value": string(kc)},
		}}}, nil
	default:
		return nil, diag.New(diag.CodeSpokeRegisterFailed,
			fmt.Sprintf("unknown engine type %q for spoke registration", engineType),
			"engine.type must be flux or argocd")
	}
}
