package cmd

import (
	"bytes"
	"context"
	"encoding/json"
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
	if len(notes) != 1 || !strings.Contains(notes[0], "argocd") ||
		!strings.Contains(notes[0], cliSecretLabel) || !strings.Contains(notes[0], packNameLabel) {
		t.Fatalf("want a deprecation note naming the pack and BOTH legacy labels, got %v", notes)
	}
}

func TestSecretsForDisplayDanglingAuthSecretRef(t *testing.T) {
	// A Pack whose authSecretRef points at a Secret that doesn't exist yet
	// (e.g. argocd-initial-admin-secret before Argo CD's first boot) must
	// not abort the whole listing: the healthy pack's row still appears,
	// and the dangling pack shows an explicit not-found marker in DATA.
	dangling := newPack("argocd", "argocd", "argocd-initial-admin-secret", map[string]string{"username": "admin"})
	healthy := newPack("gitea", "gitea", "gitea-admin-cube-idp", map[string]string{"username": "gitea_admin"})
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "gitea-admin-cube-idp", Namespace: "gitea"},
		Data:       map[string][]byte{"password": []byte("s3cr3t")},
	}
	c := newGetFakeClient(t, dangling, healthy, sec)

	rows, notes, err := secretsForDisplay(context.Background(), c, "")
	if err != nil {
		t.Fatalf("a dangling authSecretRef must not abort get secrets: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("no deprecation notes expected, got %v", notes)
	}
	if len(rows) != 2 {
		t.Fatalf("want the healthy row AND the dangling marker row, got %+v", rows)
	}
	byPack := map[string]secretRow{}
	for _, r := range rows {
		byPack[r.Pack] = r
	}
	if byPack["gitea"].Fields["password"] != "s3cr3t" {
		t.Fatalf("healthy row must be unaffected: %+v", byPack["gitea"])
	}
	d := byPack["argocd"]
	if d.Placeholder != "<secret argocd/argocd-initial-admin-secret not found>" || len(d.Fields) != 0 {
		t.Fatalf("dangling row must carry the not-found marker and NO fields (implied fields alone would read as a credential): %+v", d)
	}

	var buf strings.Builder
	printSecretRows(&buf, rows)
	if !strings.Contains(buf.String(), "<secret argocd/argocd-initial-admin-secret not found>") {
		t.Fatalf("rendered table must show the marker in DATA: %s", buf.String())
	}
}

// TestWriteSecretsJSON pins the gh-style get-secrets document (design doc
// §10): secret rows with their fields, deprecation notes carried as data, and
// a dangling authSecretRef surfaced via placeholder instead of fields.
func TestWriteSecretsJSON(t *testing.T) {
	rows := []secretRow{
		{Pack: "gitea", Namespace: "gitea", Name: "gitea-admin", Fields: map[string]string{"password": "s3cr3t"}},
		{Pack: "argocd", Namespace: "argocd", Name: "argocd-initial-admin-secret", Placeholder: "<secret argocd/argocd-initial-admin-secret not found>"},
	}
	var b bytes.Buffer
	if err := writeSecretsJSON(&b, rows, []string{"note: legacy"}); err != nil {
		t.Fatal(err)
	}
	var doc secretsDoc
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatalf("document is not valid JSON: %v\n%s", err, b.String())
	}
	if doc.V != 1 || len(doc.Secrets) != 2 || len(doc.Notes) != 1 {
		t.Fatalf("doc shape: %+v", doc)
	}
	if doc.Secrets[0].Fields["password"] != "s3cr3t" {
		t.Fatalf("fields not carried: %+v", doc.Secrets[0])
	}
	if doc.Secrets[1].Placeholder == "" || doc.Secrets[1].Fields != nil {
		t.Fatalf("dangling row must carry placeholder and no fields: %+v", doc.Secrets[1])
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
