# Companion to gen-argocd-manifests.sh: injects a cube-idp-specific
# reposerver.oci.layer.media.types data key (and explanatory comment) into
# the vendored argocd-cmd-params-cm ConfigMap document on stdin, so the
# deviation from upstream argo-cd's manifests/install.yaml is applied by
# the generator itself instead of relying on a hand-edit after every regen.
BEGIN {
    comment = "# reposerver.oci.layer.media.types (below) is a cube-idp addition to the\n" \
"# vendored upstream ConfigMap, not part of argo-cd's community install.yaml:\n" \
"# it widens argocd-repo-server's --oci-layer-media-types allow-list (see\n" \
"# cmd/argocd-repo-server/commands/argocd_repo_server.go, wired via\n" \
"# ARGOCD_REPO_SERVER_OCI_LAYER_MEDIA_TYPES / this ConfigMap key — install.yaml\n" \
"# already carries the env-from-configmap wiring, only the data key is new)\n" \
"# to also accept application/vnd.cncf.flux.content.v1.tar+gzip, the single\n" \
"# layer media type oci.PushRendered (internal/oci/push.go) writes for EVERY\n" \
"# engine, chosen so the identical artifact byte-for-byte also satisfies\n" \
"# Flux's OCIRepository reconciler. Without this, argocd-repo-server rejects\n" \
"# every cube-idp pack pull with a media-type error (verified against a real\n" \
"# kind cluster, Task 14's e2e engine matrix) — the documented, designed fix\n" \
"# (see argocd.go's package doc), applied as one field on the SAME ConfigMap\n" \
"# object already in this file (not a second partial-object apply of the\n" \
"# same name/namespace/kind), so there is no SSA field-manager/pruning risk.\n" \
"# hack/gen-argocd-manifests.sh injects this data: block itself on every\n" \
"# regen (see hack/inject-argocd-cmd-params.awk) — no hand-edit required."
    target = "  name: argocd-cmd-params-cm"
    datakey = "  reposerver.oci.layer.media.types: \"application/vnd.oci.image.layer.v1.tar+gzip,application/vnd.oci.image.layer.v1.tar,application/vnd.cncf.helm.chart.content.v1.tar+gzip,application/vnd.cncf.flux.content.v1.tar+gzip\""
    doc = ""
}
/^---$/ {
    flush()
    print "---"
    doc = ""
    next
}
# cube-idp air-gap deviation (Task 7): upstream argo-cd pins most control-plane
# containers to imagePullPolicy: Always, which makes a kubelet ignore images
# node-loaded from a vendor bundle (`up --bundle`) and reach for a registry the
# air-gapped host cannot see. Rewrite every such policy to IfNotPresent here so
# the generator itself carries the change and `--check`/regen stay consistent —
# no hand-edit survives a regeneration.
/^[[:space:]]*imagePullPolicy: Always[[:space:]]*$/ { sub(/Always/, "IfNotPresent") }
{
    if (doc == "") { doc = $0 } else { doc = doc "\n" $0 }
}
END {
    flush()
}
function flush(   is_target) {
    is_target = (index(doc, "\n" target "\n") > 0) || (doc ~ ("^" target "$")) || (doc ~ ("^" target "\n")) || (doc ~ ("\n" target "$"))
    if (is_target) {
        print comment
        gsub(target, target "\ndata:\n" datakey, doc)
    }
    printf "%s", doc
    if (doc != "") print ""
}
