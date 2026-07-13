package cmd

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterCLISecrets(t *testing.T) {
	in := []corev1.Secret{
		{ObjectMeta: metav1.ObjectMeta{Name: "gitea-admin", Namespace: "gitea",
			Labels: map[string]string{"cube-idp.dev/cli-secret": "true", "cube-idp.dev/pack-name": "gitea"}},
			Data: map[string][]byte{"username": []byte("gitea_admin"), "password": []byte("s3cr3t")}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"}},
	}
	rows := filterCLISecrets(in, "")
	if len(rows) != 1 || rows[0].Pack != "gitea" || rows[0].Fields["password"] != "s3cr3t" {
		t.Fatalf("rows: %+v", rows)
	}
	if len(filterCLISecrets(in, "argocd")) != 0 {
		t.Fatal("pack filter must exclude non-matching packs")
	}
}
