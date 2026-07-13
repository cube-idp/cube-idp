name:    "argocd"
version: "0.1.0"

// D11: argocd-initial-admin-secret is created by Argo CD itself on first
// boot (namespace "argocd" per manifests/00-namespace.yaml) and carries
// only "password" — the "admin" username is Argo CD's implicit login, so
// it's declared here rather than stored in the secret (checkpoint 0.14).
expose: {
	urls: ["https://argocd.${GATEWAY_HOST}"]
	authSecretRef: {namespace: "argocd", name: "argocd-initial-admin-secret"}
	impliedFields: {username: "admin"}
}
