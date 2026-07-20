package diag

import (
	"sort"
	"strings"
)

// Desc is the explain-facing description of a diagnostic code (rustc
// --explain pattern). Summary is the code's one-line meaning,
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
	CodeConfigRead:            {Summary: "cannot read cube.yaml"},
	CodeConfigInvalid:         {Summary: "cube.yaml YAML syntax or schema validation error"},
	CodeLockCorrupt:           {Summary: "cube.lock unreadable or corrupt"},
	CodeProviderMiss:          {Summary: "cluster provider config mismatch (e.g., render-cluster for non-kind)"},
	CodeArgoPackRedun:         {Summary: "argocd pack listed while engine.type: argocd"},
	CodeInitExists:            {Summary: "cube.yaml already exists; refusing overwrite"},
	CodeBadFlagValue:          {Summary: "an enum flag (--progress, --output) got an unrecognized value"},
	CodeGatewayPackMismatch:   {Summary: "gateway.ref points at a pack whose pack.cue name != gateway.pack"},
	CodeUpgradeGuard:          {Summary: "upgrade refused: see summary (was the last un-coded user-facing error)"},
	CodeConfirmRequired:       {Summary: "a destructive command refused to run without confirmation (--yes / --confirm)"},
	CodeProviderConfigRemoved: {Summary: "cluster.providerConfig was replaced by providerConfigRef/forProvider (migration required)"},
	CodeEngineTuningRemoved:   {Summary: "engine.tuning was removed (engine-as-pack) — move the knobs to engine.values as chart values of the cube-engine-<type> pack"},
	CodeEnginePackMismatch:    {Summary: "engine.ref points at a pack whose pack.cue name != cube-engine-<engine.type>"},

	// 01xx: doctor preflight checks
	CodeDoctorRuntime: {Summary: "container runtime not found"},
	CodeDoctorPort:    {Summary: "required host port already in use"},
	CodeDoctorDisk:    {Summary: "low disk space in cache directory (warning)"},
	CodeDoctorInotify: {Summary: "inotify limits too low (warning, Linux-only)"},
	CodeDoctorGitCLI:  {Summary: "git CLI missing while git-sourced packs are configured (doctor, warning)"},

	// 1xxx: cluster
	CodeClusterTypeUnknown:     {Summary: "cluster provider type unknown or unsupported"},
	CodeClusterFieldsConflict:  {Summary: "node-creation fields (extraPorts/mounts/providerConfig/kubernetesVersion) set with provider: existing (config cross-validation)"},
	CodeClusterNotExists:       {Summary: "cluster does not exist; run `cube-idp up`"},
	CodeProviderConfigRefFetch: {Summary: "providerConfigRef fetch failed, unparsable, or not exactly one YAML mapping document"},

	// 11xx: kubeconfig/connectivity
	CodeKubeUnreachable: {Summary: "cluster behind context unreachable"},
	CodeKubeconfigError: {Summary: "kubeconfig load/parse/validation error"},

	// 12xx: kind provider
	CodeKindConfigMerge:   {Summary: "kind providerConfig merge failed"},
	CodeKindConfigInvalid: {Summary: "kind providerConfig structure invalid"},
	CodeKindCreateFailed:  {Summary: "kind cluster creation failed"},
	CodeKindKubeconfigGet: {Summary: "cannot get kubeconfig from kind"},
	CodeKindDeleteFailed:  {Summary: "kind cluster deletion failed"},
	CodeKindCoreOverride:  {Summary: "kind core injection overrode a user providerConfigRef/forProvider field (warning)"},

	// 13xx: k3d provider
	CodeK3dConfigMerge:   {Summary: "k3d providerConfig merge failed"},
	CodeK3dConfigInvalid: {Summary: "k3d providerConfig structure invalid"},
	CodeK3dCreateFailed:  {Summary: "k3d cluster creation failed / runtime unreachable"},
	CodeK3dKubeconfigGet: {Summary: "cannot get kubeconfig from k3d"},
	CodeK3dDeleteFailed:  {Summary: "k3d cluster deletion failed"},
	CodeK3dCoreOverride:  {Summary: "k3d core injection overrode a user providerConfigRef/forProvider field (warning)"},

	// 2xxx: apply
	CodeApplyWaitTimeout: {Summary: "timed out waiting for resources to become ready"},
	CodeApplyClientBuild: {Summary: "cannot build Kubernetes client"},
	CodeApplyFailed:      {Summary: "server-side apply failed"},
	CodeInventoryFailed:  {Summary: "inventory read/write/parse failed"},
	CodeApplyDiffFailed:  {Summary: "server-side diff failed"},
	CodeApplyPruneFailed: {Summary: "prune delete of untracked objects failed"},
	CodeApplyParseYAML:   {Summary: "cannot parse manifest YAML"},

	// 3xxx: engine
	CodeEngineTypeUnknown:   {Summary: "unknown engine type in config"},
	CodeEngineManifestsInv:  {Summary: "embedded engine install manifests invalid (RETIRED 2026-07-19 by engine-as-pack — install left the engine seam)"},
	CodeEngineHealthTimeout: {Summary: "engine health check timed out or components not ready"},
	CodeEngineUninstallFail: {Summary: "flux prune/uninstall timeout"},
	CodeEngineArgocdRegFail: {Summary: "reserved: argocd gitea-fallback capability check, unbuilt by design"},
	CodePokeTargetMissing:   {Summary: "Poke found no delivery source (OCIRepository/GitRepository/Application) for the pack"},
	CodePokeIOFail:          {Summary: "Poke found the delivery source but could not read/update it (transient engine IO — retry)"},
	CodeEngineTuningUnknown: {Summary: "engine.tuning.components names a component the engine's install manifests don't have (or its Deployment cannot be patched) (RETIRED 2026-07-19 by engine-as-pack — never emitted since)"},
	// Opt-in engine self-management (spec.engine.selfManage): up pushes the
	// rendered engine manifests to zot and attaches an engine-native
	// self-source, so the engine reconciles itself from then on.
	CodeEngineSelfManage: {Summary: "engine.selfManage failed: cube-engine artifact push, self-source build/apply, or post-attach health wait — re-run `cube-idp up`"},
	CodeEngineDepWait:    {Summary: "a pack's dependency did not become healthy before its wave-gated delivery (argocd)"},

	// 4xxx: pack
	CodePackRefInvalid:   {Summary: "unsupported pack ref scheme"},
	CodePackValuesInv:    {Summary: "pack values decode or type validation error"},
	CodePackCueInvalid:   {Summary: "pack.cue missing, syntax error, or compilation failure"},
	CodePackManifestErr:  {Summary: "pack manifests/ directory read or YAML parse error"},
	CodePackChartErr:     {Summary: "Helm chart load/parse/render error"},
	CodePackFetchFail:    {Summary: "remote pack source fetch/resolution failed"},
	CodePackRefUnpin:     {Summary: "remote pack ref not pinned (missing @<rev> or :tag)"},
	CodePackKustomizeErr: {Summary: "kustomize render failed"},
	CodePackCnoeInvalid:  {Summary: "cnoe-compat document invalid or unsupported"},
	CodePackCnoeUnres:    {Summary: "cnoe:// path unresolvable"},
	CodePackExposeInv:    {Summary: "expose: block in pack.cue invalid"},
	CodePackOCIErr:       {Summary: "OCI pack pull/extract error (pullOCI failures)"},
	CodePackCacheDirErr:  {Summary: "cache directory access/creation error"},
	CodePackGuardTrip:    {Summary: "extraction guard tripped (path traversal/symlink)"},
	CodePackPushFail:     {Summary: "pack push (directory archive, OCI push, or tag) failed"},
	// The values rule: values: means helm values, only, always — consumed
	// exclusively by a pack's chart.yaml render. packs[].extraManifests is
	// the uniform extras mechanism, valid for every pack kind.
	CodePackValuesChartless: {Summary: "values: set on a pack without chart.yaml — values are helm values only; use packs[].extraManifests for raw resources"},
	CodePackExtraManifests:  {Summary: "packs[].extraManifests is not valid multi-doc YAML"},
	CodePackDepUnknown:      {Summary: "dependsOn names a pack not in this cube"},
	CodePackDepCycle:        {Summary: "pack dependency cycle (the message shows the path)"},
	CodePackDepGateway:      {Summary: "gateway pack cannot carry a dependsOn of its own"},

	// 5xxx: registry
	CodeZotManifestsInv:         {Summary: "embedded zot manifests invalid"},
	CodePortForwardFail:         {Summary: "port-forward to registry failed"},
	CodeOCIPushFail:             {Summary: "OCI push (artifact staging or push) failed"},
	CodeDigestResolveFail:       {Summary: "remote digest resolution failed (upgrade --plan)"},
	CodeRegistryRouteCRDTimeout: {Summary: "Gateway API HTTPRoute CRD not Established before the registry HTTPRoute apply"},

	// 6xxx: trust/hostname
	CodeTrustCAFail:        {Summary: "local CA creation/load failed"},
	CodeTrustOSStoreFail:   {Summary: "OS trust-store install failed"},
	CodeTrustOSStoreRevert: {Summary: "OS trust-store uninstall/revert failed"},
	CodeTrustCoreDNSFail:   {Summary: "CoreDNS rewrite patch failed or did not roll out"},
	CodeTrustCertIssueFail: {Summary: "server certificate issuance failed"},
	CodeTrustStateFail:     {Summary: "trust state file corrupt"},

	// 70xx: vendor / air-gap bundle
	CodeVendorLockMissing:   {Summary: "cube.lock missing, unreadable, or corrupt (vendor)"},
	CodeVendorPullFail:      {Summary: "vendor: pull of a pinned pack/image, or writing the bundle itself, failed — produce side (vendor); the consume-side load is CUBE-7006 (bundle is complete-or-error, never partial)"},
	CodeVendorBundleCorrupt: {Summary: "vendor bundle is unreadable or corrupt (Open)"},
	CodeVendorIncomplete:    {Summary: "vendor bundle is missing or has corrupt content for a locked pack or image (Verify)"},
	CodeBundleNoImageLoader: {Summary: "`up --bundle` needs a provider that node-loads images (kind/k3d); `existing` cannot"},
	CodeBundleImageLoadFail: {Summary: "bundled image load into cluster nodes failed (kind/k3d LoadImages, consume side)"},

	// 71xx: exec-plugin discovery
	CodePluginNotFound:    {Summary: "unknown command and no cube-idp-<name> plugin found on PATH"},
	CodePluginTrustIO:     {Summary: "plugin trust store (~/.config/cube-idp/trust.json) read/write/hash error"},
	CodePluginExecFail:    {Summary: "plugin process failed to start/run (not the plugin's own reported exit code)"},
	CodePluginUntrusted:   {Summary: "plugin refused: unknown or changed sha256, and no interactive confirmation"},
	CodePluginNameInvalid: {Summary: "plugin name fails the ^[a-z0-9][a-z0-9-]*$ charset guard on `plugin install`/`plugin trust`"},
	CodePluginNoPlatform:  {Summary: "the official plugin index has no build for this GOOS/GOARCH (`plugin install`)"},

	// 72xx: sync
	CodeSyncNoManifests:    {Summary: "sync dir has no pack.cue and no renderable *.yaml manifests"},
	CodeSyncWatchSetupFail: {Summary: "`sync --watch` cannot start or attach the filesystem watcher"},

	// 73xx: repo
	CodeRepoGiteaUnavailable: {Summary: "gitea admin secret missing or port-forward to the gitea pod failed"},
	CodeRepoGiteaAPIFail:     {Summary: "gitea REST API returned an unexpected status (create/fetch repo)"},
	CodeRepoDeployFail:       {Summary: "repo created but engine git source registration/apply failed"},
	// The gitea guarantee: any pack with delivery: repo implicitly depends on
	// the gitea pack, so gitea must be in spec.packs and cannot itself be
	// repo-delivered. Raised at config load.
	CodeRepoDeliveryConfig: {Summary: "delivery: repo needs the gitea pack in spec.packs, and gitea itself cannot be repo-delivered"},

	// 8xxx: spoke
	CodeSpokeProviderUnsupported: {Summary: "spoke cluster.provider invalid for spokes (k3d deferred; existing needs context; duplicate name)"},
	CodeSpokeBootstrapFailed:     {Summary: "spoke RBAC bootstrap apply failed"},
	CodeSpokeTokenFailed:         {Summary: "spoke ServiceAccount token issuance failed"},
	CodeSpokeEnsureFailed:        {Summary: "spoke cluster create/connect failed"},
	CodeSpokeRegisterFailed:      {Summary: "hub registration secret build/apply failed"},
	CodeSpokeUnreachable:         {Summary: "spoke hub registration missing, or spoke API server unreachable from this machine"},
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
	"6": "6xxx: trust/hostname",

	"8": "8xxx: spoke",

	"01": "01xx: doctor preflight checks",
	"11": "11xx: kubeconfig/connectivity",
	"12": "12xx: kind provider",
	"13": "13xx: k3d provider",
	"70": "70xx: vendor / air-gap bundle",
	"71": "71xx: exec-plugin discovery",
	"72": "72xx: sync",
	"73": "73xx: repo",
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
