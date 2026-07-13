name:    "gitea"
version: "0.1.0"
#Values: {}

// D11: the CLI-consumable admin credential lives in
// manifests/10-secret.yaml (gitea-admin-cube-idp, username gitea_admin —
// checkpoint 0.14/0.17).
expose: {
	urls: ["https://gitea.${GATEWAY_HOST}"]
	authSecretRef: {namespace: "gitea", name: "gitea-admin-cube-idp"}
	impliedFields: {username: "gitea_admin"}
}
