package diag

import (
	"sort"
	"strings"
)

// Desc is the explain-facing description of a diagnostic code (rustc
// --explain pattern, spec WP8). Summary is the code's one-line meaning,
// lifted verbatim from the constant's comment in codes.go. Detail and
// Remediation are optional richer prose — call sites attach the
// situation-specific remediation at error time, so an empty field here
// means "nothing beyond the summary yet" and renderers omit the line.
type Desc struct {
	Summary     string
	Detail      string
	Remediation string
}

// registry keys by the typed constants, not string literals, so an entry
// cannot outlive its code; TestRegistryCoversEveryDeclaredCode holds the
// other direction (a code cannot ship without an entry).
var registry = map[Code]Desc{
	// 0xxx: preflight/config
	CodeConfigRead:          {Summary: "cannot read cube.yaml"},
	CodeConfigInvalid:       {Summary: "cube.yaml YAML syntax or schema validation error"},
	CodeLockCorrupt:         {Summary: "cube.lock unreadable or corrupt (Phase 2)"},
	CodeProviderMiss:        {Summary: "cluster provider config mismatch (e.g., render-cluster for non-kind)"},
	CodeArgoPackRedun:       {Summary: "argocd pack listed while engine.type: argocd (Phase 2)"},
	CodeInitExists:          {Summary: "cube.yaml already exists; refusing overwrite"},
	CodeBadFlagValue:        {Summary: "an enum flag (--progress, --output) got an unrecognized value"},
	CodeGatewayPackMismatch: {Summary: "gateway.ref points at a pack whose pack.cue name != gateway.pack (F11: ref silently wins over pack)"},
	CodeUpgradeGuard:        {Summary: "upgrade refused: see summary (was the last un-coded user-facing error)"},
	CodeConfirmRequired:     {Summary: "a destructive command refused to run without confirmation (--yes / --confirm)"},

	// 01xx: doctor preflight checks (Phase 2)
	CodeDoctorRuntime: {Summary: "container runtime not found"},
	CodeDoctorPort:    {Summary: "required host port already in use"},
	CodeDoctorDisk:    {Summary: "low disk space in cache directory (warning)"},
	CodeDoctorInotify: {Summary: "inotify limits too low (warning, Linux-only)"},
	CodeDoctorGitCLI:  {Summary: "git CLI missing while git-sourced packs are configured (doctor, warning)"},

	// 1xxx: cluster
	CodeClusterTypeUnknown:    {Summary: "cluster provider type unknown or unsupported"},
	CodeClusterFieldsConflict: {Summary: "node-creation fields (extraPorts/mounts/providerConfig/kubernetesVersion) set with provider: existing (config cross-validation)"},
	CodeClusterNotExists:      {Summary: "cluster does not exist; run `cube-idp up`"},

	// 11xx: kubeconfig/connectivity
	CodeKubeUnreachable: {Summary: "cluster behind context unreachable"},
	CodeKubeconfigError: {Summary: "kubeconfig load/parse/validation error"},

	// 12xx: kind provider
	CodeKindConfigMerge:   {Summary: "kind providerConfig merge failed"},
	CodeKindConfigInvalid: {Summary: "kind providerConfig structure invalid"},
	CodeKindCreateFailed:  {Summary: "kind cluster creation failed"},
	CodeKindKubeconfigGet: {Summary: "cannot get kubeconfig from kind"},
	CodeKindDeleteFailed:  {Summary: "kind cluster deletion failed"},

	// 13xx: k3d provider
	CodeK3dConfigMerge:   {Summary: "k3d providerConfig merge failed"},
	CodeK3dConfigInvalid: {Summary: "k3d providerConfig structure invalid"},
	CodeK3dCreateFailed:  {Summary: "k3d cluster creation failed / runtime unreachable"},
	CodeK3dKubeconfigGet: {Summary: "cannot get kubeconfig from k3d"},
	CodeK3dDeleteFailed:  {Summary: "k3d cluster deletion failed"},

	// 2xxx: apply
	CodeApplyWaitTimeout: {Summary: "timed out waiting for resources to become ready"},
	CodeApplyClientBuild: {Summary: "cannot build Kubernetes client"},
	CodeApplyFailed:      {Summary: "server-side apply failed"},
	CodeInventoryFailed:  {Summary: "inventory read/write/parse failed"},
	CodeApplyDiffFailed:  {Summary: "server-side diff failed (Phase 2)"},
	CodeApplyPruneFailed: {Summary: "prune delete of untracked objects failed"},
	CodeApplyParseYAML:   {Summary: "cannot parse manifest YAML"},

	// 3xxx: engine
	CodeEngineTypeUnknown:   {Summary: "unknown engine type in config"},
	CodeEngineManifestsInv:  {Summary: "embedded engine install manifests invalid"},
	CodeEngineHealthTimeout: {Summary: "engine health check timed out or components not ready"},
	CodeEngineUninstallFail: {Summary: "flux prune/uninstall timeout"},
	CodeEngineArgocdRegFail: {Summary: "reserved: argocd gitea-fallback capability check (spec §7), unbuilt by design"},
	CodePokeTargetMissing:   {Summary: "Poke found no delivery source (OCIRepository/GitRepository/Application) for the pack"},
	CodePokeIOFail:          {Summary: "Poke found the delivery source but could not read/update it (transient engine IO — retry)"},
	CodeEngineTuningUnknown: {Summary: "engine.tuning.components names a component the engine's install manifests don't have (or its Deployment cannot be patched)"},

	// 4xxx: pack
	CodePackRefInvalid:   {Summary: "unsupported pack ref scheme"},
	CodePackValuesInv:    {Summary: "pack values decode or type validation error"},
	CodePackCueInvalid:   {Summary: "pack.cue missing, syntax error, or compilation failure"},
	CodePackManifestErr:  {Summary: "pack manifests/ directory read or YAML parse error"},
	CodePackChartErr:     {Summary: "Helm chart load/parse/render error"},
	CodePackFetchFail:    {Summary: "remote pack source fetch/resolution failed (Phase 2)"},
	CodePackRefUnpin:     {Summary: "remote pack ref not pinned (missing @<rev> or :tag) (Phase 2)"},
	CodePackKustomizeErr: {Summary: "kustomize render failed (Phase 2)"},
	CodePackCnoeInvalid:  {Summary: "cnoe-compat document invalid or unsupported (Phase 2)"},
	CodePackCnoeUnres:    {Summary: "cnoe:// path unresolvable (Phase 2)"},
	CodePackExposeInv:    {Summary: "expose: block in pack.cue invalid (Phase 2)"},
	CodePackOCIErr:       {Summary: "OCI pack pull/extract error (pullOCI failures)"},
	CodePackCacheDirErr:  {Summary: "cache directory access/creation error"},
	CodePackGuardTrip:    {Summary: "extraction guard tripped (path traversal/symlink) (Phase 2)"},
	CodePackPushFail:     {Summary: "pack push (directory archive, OCI push, or tag) failed"},

	// 5xxx: registry
	CodeZotManifestsInv:         {Summary: "embedded zot manifests invalid"},
	CodePortForwardFail:         {Summary: "port-forward to registry failed"},
	CodeOCIPushFail:             {Summary: "OCI push (artifact staging or push) failed"},
	CodeDigestResolveFail:       {Summary: "remote digest resolution failed (upgrade --plan) (Phase 2)"},
	CodeRegistryRouteCRDTimeout: {Summary: "Gateway API HTTPRoute CRD not Established before the registry HTTPRoute apply"},

	// 6xxx: trust/hostname (Phase 2)
	CodeTrustCAFail:        {Summary: "local CA creation/load failed (Phase 2)"},
	CodeTrustOSStoreFail:   {Summary: "OS trust-store install failed (Phase 2)"},
	CodeTrustOSStoreRevert: {Summary: "OS trust-store uninstall/revert failed (Phase 2)"},
	CodeTrustCoreDNSFail:   {Summary: "CoreDNS rewrite patch failed or did not roll out (Phase 2)"},
	CodeTrustCertIssueFail: {Summary: "server certificate issuance failed (Phase 2)"},
	CodeTrustStateFail:     {Summary: "trust state file corrupt (Phase 2)"},

	// 70xx: vendor / air-gap bundle (spec §4.1, Phase 3)
	CodeVendorLockMissing:   {Summary: "cube.lock missing, unreadable, or corrupt (vendor)"},
	CodeVendorPullFail:      {Summary: "vendor: pull of a pinned pack/image, or writing the bundle itself, failed — produce side (vendor); the consume-side load is CUBE-7006 (bundle is complete-or-error, never partial)"},
	CodeVendorBundleCorrupt: {Summary: "vendor bundle is unreadable or corrupt (Open)"},
	CodeVendorIncomplete:    {Summary: "vendor bundle is missing or has corrupt content for a locked pack or image (Verify)"},
	CodeBundleNoImageLoader: {Summary: "`up --bundle` needs a provider that node-loads images (kind/k3d); `existing` cannot"},
	CodeBundleImageLoadFail: {Summary: "bundled image load into cluster nodes failed (kind/k3d LoadImages, consume side)"},

	// 71xx: exec-plugin discovery (spec §4.4 tier 2, Phase 3)
	CodePluginNotFound:    {Summary: "unknown command and no cube-idp-<name> plugin found on PATH"},
	CodePluginTrustIO:     {Summary: "plugin trust store (~/.config/cube-idp/trust.json) read/write/hash error"},
	CodePluginExecFail:    {Summary: "plugin process failed to start/run (not the plugin's own reported exit code)"},
	CodePluginUntrusted:   {Summary: "plugin refused: unknown or changed sha256, and no interactive confirmation"},
	CodePluginNameInvalid: {Summary: "plugin name fails the ^[a-z0-9][a-z0-9-]*$ charset guard on `plugin install`/`plugin trust`"},

	// 72xx: sync (Task 10, Task 11)
	CodeSyncNoManifests:    {Summary: "sync dir has no pack.cue and no renderable *.yaml manifests"},
	CodeSyncWatchSetupFail: {Summary: "`sync --watch` cannot start or attach the filesystem watcher"},

	// 73xx: repo (Task 12)
	CodeRepoGiteaUnavailable: {Summary: "gitea admin secret missing or port-forward to the gitea pod failed"},
	CodeRepoGiteaAPIFail:     {Summary: "gitea REST API returned an unexpected status (create/fetch repo)"},
	CodeRepoDeployFail:       {Summary: "repo created but engine git source registration/apply failed"},

	// 8xxx: spoke (Phase 5)
	CodeSpokeProviderUnsupported: {Summary: "spoke cluster.provider invalid for spokes (k3d deferred; existing needs context; duplicate name)"},
	CodeSpokeBootstrapFailed:     {Summary: "spoke RBAC bootstrap apply failed"},
	CodeSpokeTokenFailed:         {Summary: "spoke ServiceAccount token issuance failed"},
}

// ranges carries the documented meaning of each numeric range, verbatim
// from codes.go's section comments; two-digit prefixes are the specific
// sub-ranges and win over their one-digit parent.
var ranges = map[string]string{
	"0": "0xxx: preflight/config",
	"1": "1xxx: cluster",
	"2": "2xxx: apply",
	"3": "3xxx: engine",
	"4": "4xxx: pack",
	"5": "5xxx: registry",
	"6": "6xxx: trust/hostname (Phase 2)",

	"8": "8xxx: spoke (Phase 5)",

	"01": "01xx: doctor preflight checks (Phase 2)",
	"11": "11xx: kubeconfig/connectivity",
	"12": "12xx: kind provider",
	"13": "13xx: k3d provider",
	"70": "70xx: vendor / air-gap bundle (spec §4.1, Phase 3)",
	"71": "71xx: exec-plugin discovery (spec §4.4 tier 2, Phase 3)",
	"72": "72xx: sync (Task 10, Task 11)",
	"73": "73xx: repo (Task 12)",
}

// AllCodes returns every registered code, sorted.
func AllCodes() []Code {
	codes := make([]Code, 0, len(registry))
	for c := range registry {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}

// Describe looks up a code's description.
func Describe(c Code) (Desc, bool) {
	d, ok := registry[c]
	return d, ok
}

// RangeMeaning returns the documented meaning of the code's numeric range,
// or "" for a malformed/unknown code. The digits are taken after the first
// hyphen rather than after a literal prefix — codes_test.go bans the quoted
// code-prefix literal outside codes.go itself.
func RangeMeaning(c Code) string {
	_, digits, ok := strings.Cut(string(c), "-")
	if !ok || len(digits) < 2 {
		return ""
	}
	if m, ok := ranges[digits[:2]]; ok {
		return m
	}
	return ranges[digits[:1]]
}
