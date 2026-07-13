package cmd

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

// newPack builds an unstructured Pack record (D11) with the given name and,
// optionally, an authSecretRef + impliedFields — mirroring what
// pack.PackObject writes.
func newPack(name, secNamespace, secName string, implied map[string]string) *unstructured.Unstructured {
	spec := map[string]any{"version": "0.1.0", "ready": true}
	if secNamespace != "" {
		spec["authSecretRef"] = map[string]any{"namespace": secNamespace, "name": secName}
		spec["authSecret"] = secNamespace + "/" + secName
	}
	if len(implied) > 0 {
		f := map[string]any{}
		for k, v := range implied {
			f[k] = v
		}
		spec["impliedFields"] = f
	}
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cube-idp.dev/v1alpha1",
		"kind":       "Pack",
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
	}}
	return u
}

func newGetFakeClient(t *testing.T, objs ...runtime.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "Pack"}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "PackList"}, &unstructured.UnstructuredList{})
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}

func TestSecretsForDisplayPrimaryPath(t *testing.T) {
	pack := newPack("gitea", "gitea", "gitea-admin-cube-idp", map[string]string{"username": "gitea_admin"})
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "gitea-admin-cube-idp", Namespace: "gitea"},
		Data:       map[string][]byte{"password": []byte("s3cr3t")},
	}
	c := newGetFakeClient(t, pack, sec)

	rows, notes, err := secretsForDisplay(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("primary path must not produce a deprecation note, got %v", notes)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %+v", rows)
	}
	r := rows[0]
	if r.Pack != "gitea" || r.Namespace != "gitea" || r.Name != "gitea-admin-cube-idp" {
		t.Fatalf("row identity: %+v", r)
	}
	if r.Fields["password"] != "s3cr3t" {
		t.Fatalf("secret's own data must appear: %+v", r.Fields)
	}
	if r.Fields["username"] != "gitea_admin" {
		t.Fatalf("impliedFields must be merged in: %+v", r.Fields)
	}

	var buf strings.Builder
	printSecretRows(&buf, rows)
	if !strings.Contains(buf.String(), "gitea_admin") {
		t.Fatalf("rendered table must contain gitea_admin: %s", buf.String())
	}
}

func TestSecretsForDisplayLegacyFallback(t *testing.T) {
	// A pack with no Pack record at all (or one without authSecretRef) must
	// still surface its label-only secret, with a deprecation note.
	legacySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-initial-admin-secret", Namespace: "argocd",
			Labels: map[string]string{cliSecretLabel: "true", packNameLabel: "argocd"}},
		Data: map[string][]byte{"password": []byte("s3cr3t")},
	}
	c := newGetFakeClient(t, legacySecret)

	rows, notes, err := secretsForDisplay(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Pack != "argocd" {
		t.Fatalf("want the legacy row surfaced: %+v", rows)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "legacy cli-secret label") || !strings.Contains(notes[0], "argocd") {
		t.Fatalf("want a deprecation note naming the pack, got %v", notes)
	}
}

func TestSecretsForDisplayPrimaryPathSkipsLegacyDuplicate(t *testing.T) {
	// A pack mid-migration: the Secret still carries the legacy label (not
	// yet removed) AND the pack now declares expose.authSecretRef pointing
	// at that same Secret. It must be surfaced exactly once, via the
	// primary path, with no deprecation note.
	pack := newPack("gitea", "gitea", "gitea-admin-cube-idp", map[string]string{"username": "gitea_admin"})
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "gitea-admin-cube-idp", Namespace: "gitea",
			Labels: map[string]string{cliSecretLabel: "true", packNameLabel: "gitea"}},
		Data: map[string][]byte{"password": []byte("s3cr3t")},
	}
	c := newGetFakeClient(t, pack, sec)

	rows, notes, err := secretsForDisplay(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("a pack resolved via its own expose.authSecretRef must not get a deprecation note: %v", notes)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 row (no duplicate), got %+v", rows)
	}
}
