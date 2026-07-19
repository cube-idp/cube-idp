// Package diag defines cube-idp's error codes as typed constants.
package diag

// 0xxx: preflight/config
const (
	CodeConfigRead            Code = "CUBE-0001" // cannot read cube.yaml
	CodeConfigInvalid         Code = "CUBE-0002" // cube.yaml YAML syntax or schema validation error
	CodeLockCorrupt           Code = "CUBE-0003" // cube.lock unreadable or corrupt (Phase 2)
	CodeProviderMiss          Code = "CUBE-0004" // cluster provider config mismatch (e.g., render-cluster for non-kind)
	CodeArgoPackRedun         Code = "CUBE-0005" // argocd pack listed while engine.type: argocd (Phase 2)
	CodeInitExists            Code = "CUBE-0006" // cube.yaml already exists; refusing overwrite
	CodeBadFlagValue          Code = "CUBE-0007" // an enum flag (--progress, --output) got an unrecognized value
	CodeGatewayPackMismatch   Code = "CUBE-0008" // gateway.ref points at a pack whose pack.cue name != gateway.pack (F11: ref silently wins over pack)
	CodeUpgradeGuard          Code = "CUBE-0009" // upgrade refused: see summary (was the last un-coded user-facing error)
	CodeConfirmRequired       Code = "CUBE-0010" // a destructive command refused to run without confirmation (--yes / --confirm)
	CodeProviderConfigRemoved Code = "CUBE-0011" // cluster.providerConfig was replaced by providerConfigRef/forProvider (migration required)
	CodeEngineTuningRemoved   Code = "CUBE-0012" // engine.tuning was removed (engine-as-pack) — use engine.values (chart values of the cube-engine-<type> pack)
	CodeEnginePackMismatch    Code = "CUBE-0013" // engine.ref points at a pack whose pack.cue name != cube-engine-<engine.type>
)

// 01xx: doctor preflight checks (Phase 2)
const (
	CodeDoctorRuntime Code = "CUBE-0101" // container runtime not found
	CodeDoctorPort    Code = "CUBE-0102" // required host port already in use
	CodeDoctorDisk    Code = "CUBE-0103" // low disk space in cache directory (warning)
	CodeDoctorInotify Code = "CUBE-0104" // inotify limits too low (warning, Linux-only)
	CodeDoctorGitCLI  Code = "CUBE-0105" // git CLI missing while git-sourced packs are configured (doctor, warning)
)

// 1xxx: cluster
const (
	CodeClusterTypeUnknown     Code = "CUBE-1001" // cluster provider type unknown or unsupported
	CodeClusterFieldsConflict  Code = "CUBE-1003" // node-creation fields (extraPorts/mounts/providerConfig/kubernetesVersion) set with provider: existing (config cross-validation)
	CodeClusterNotExists       Code = "CUBE-1004" // cluster does not exist; run `cube-idp up`
	CodeProviderConfigRefFetch Code = "CUBE-1005" // providerConfigRef fetch failed, unparsable, or not exactly one YAML mapping document
)

// 11xx: kubeconfig/connectivity
const (
	CodeKubeUnreachable Code = "CUBE-1101" // cluster behind context unreachable
	CodeKubeconfigError Code = "CUBE-1102" // kubeconfig load/parse/validation error
)

// 12xx: kind provider
const (
	CodeKindConfigMerge   Code = "CUBE-1201" // kind providerConfig merge failed
	CodeKindConfigInvalid Code = "CUBE-1202" // kind providerConfig structure invalid
	CodeKindCreateFailed  Code = "CUBE-1203" // kind cluster creation failed
	CodeKindKubeconfigGet Code = "CUBE-1204" // cannot get kubeconfig from kind
	CodeKindDeleteFailed  Code = "CUBE-1205" // kind cluster deletion failed
	CodeKindCoreOverride  Code = "CUBE-1206" // kind core injection overrode a user providerConfigRef/forProvider field (warning)
)

// 13xx: k3d provider
const (
	CodeK3dConfigMerge   Code = "CUBE-1301" // k3d providerConfig merge failed
	CodeK3dConfigInvalid Code = "CUBE-1302" // k3d providerConfig structure invalid
	CodeK3dCreateFailed  Code = "CUBE-1303" // k3d cluster creation failed / runtime unreachable
	CodeK3dKubeconfigGet Code = "CUBE-1304" // cannot get kubeconfig from k3d
	CodeK3dDeleteFailed  Code = "CUBE-1305" // k3d cluster deletion failed
	CodeK3dCoreOverride  Code = "CUBE-1306" // k3d core injection overrode a user providerConfigRef/forProvider field (warning)
)

// 2xxx: apply
const (
	CodeApplyWaitTimeout Code = "CUBE-2001" // timed out waiting for resources to become ready
	CodeApplyClientBuild Code = "CUBE-2002" // cannot build Kubernetes client
	CodeApplyFailed      Code = "CUBE-2003" // server-side apply failed
	CodeInventoryFailed  Code = "CUBE-2004" // inventory read/write/parse failed
	CodeApplyDiffFailed  Code = "CUBE-2005" // server-side diff failed (Phase 2)
	CodeApplyPruneFailed Code = "CUBE-2006" // prune delete of untracked objects failed
	CodeApplyParseYAML   Code = "CUBE-2007" // cannot parse manifest YAML
)

// 3xxx: engine
const (
	CodeEngineTypeUnknown   Code = "CUBE-3001" // unknown engine type in config
	CodeEngineManifestsInv  Code = "CUBE-3003" // embedded engine install manifests invalid (RETIRED 2026-07-19 by engine-as-pack — install left the engine seam)
	CodeEngineHealthTimeout Code = "CUBE-3004" // engine health check timed out or components not ready
	CodeEngineUninstallFail Code = "CUBE-3005" // flux prune/uninstall timeout
	CodeEngineArgocdRegFail Code = "CUBE-3006" // reserved: argocd gitea-fallback capability check (spec §7), unbuilt by design
	CodePokeTargetMissing   Code = "CUBE-3007" // Poke found no delivery source (OCIRepository/GitRepository/Application) for the pack
	CodePokeIOFail          Code = "CUBE-3008" // Poke found the delivery source but could not read/update it (transient engine IO — retry)
	CodeEngineTuningUnknown Code = "CUBE-3009" // engine.tuning.components names a component the engine's install manifests don't have (or its Deployment cannot be patched) (RETIRED 2026-07-19 by engine-as-pack — never emitted since)
	// GT16 engine self-management (Phase 5 P8):
	CodeEngineSelfManage Code = "CUBE-3010" // engine.selfManage failed: cube-engine artifact push, self-source build/apply, or post-attach health wait — re-run `cube-idp up`
	CodeEngineDepWait    Code = "CUBE-3011" // a pack's dependency did not become healthy before its wave-gated delivery (argocd)
)

// 4xxx: pack
const (
	CodePackRefInvalid   Code = "CUBE-4001" // unsupported pack ref scheme
	CodePackValuesInv    Code = "CUBE-4002" // pack values decode or type validation error
	CodePackCueInvalid   Code = "CUBE-4003" // pack.cue missing, syntax error, or compilation failure
	CodePackManifestErr  Code = "CUBE-4004" // pack manifests/ directory read or YAML parse error
	CodePackChartErr     Code = "CUBE-4005" // Helm chart load/parse/render error
	CodePackFetchFail    Code = "CUBE-4006" // remote pack source fetch/resolution failed (Phase 2)
	CodePackRefUnpin     Code = "CUBE-4007" // remote pack ref not pinned (missing @<rev> or :tag) (Phase 2)
	CodePackKustomizeErr Code = "CUBE-4008" // kustomize render failed (Phase 2)
	CodePackCnoeInvalid  Code = "CUBE-4009" // cnoe-compat document invalid or unsupported (Phase 2)
	CodePackCnoeUnres    Code = "CUBE-4010" // cnoe:// path unresolvable (Phase 2)
	CodePackExposeInv    Code = "CUBE-4011" // expose: block in pack.cue invalid (Phase 2)
	CodePackOCIErr       Code = "CUBE-4012" // OCI pack pull/extract error (pullOCI failures)
	CodePackCacheDirErr  Code = "CUBE-4013" // cache directory access/creation error
	CodePackGuardTrip    Code = "CUBE-4014" // extraction guard tripped (path traversal/symlink) (Phase 2)
	CodePackPushFail     Code = "CUBE-4015" // pack push (directory archive, OCI push, or tag) failed
	// GT15 values stone (Phase 5 U4): values: are helm values only; the
	// uniform extras channel for every pack kind is packs[].extraManifests.
	CodePackValuesChartless Code = "CUBE-4016" // values: set on a pack without chart.yaml (values are helm-only, GT15)
	CodePackExtraManifests  Code = "CUBE-4017" // packs[].extraManifests is not valid multi-doc YAML
	// Pack dependencies (p6 DEP1, spec 2026-07-19 §3).
	CodePackDepUnknown Code = "CUBE-4018" // dependsOn names a pack not in this cube
	CodePackDepCycle   Code = "CUBE-4019" // pack dependency cycle (the message shows the path)
	CodePackDepGateway Code = "CUBE-4020" // gateway pack cannot carry a dependsOn of its own
	// Remote values (spec 2026-07-19 §5.1, §8).
	CodePackValuesRefFetch Code = "CUBE-4021" // packs[].valuesRef fetch failed, not a YAML mapping, or merge with inline values failed
)

// 5xxx: registry
const (
	CodeZotManifestsInv         Code = "CUBE-5001" // embedded zot manifests invalid
	CodePortForwardFail         Code = "CUBE-5002" // port-forward to registry failed
	CodeOCIPushFail             Code = "CUBE-5003" // OCI push (artifact staging or push) failed
	CodeDigestResolveFail       Code = "CUBE-5004" // remote digest resolution failed (upgrade --plan) (Phase 2)
	CodeRegistryRouteCRDTimeout Code = "CUBE-5005" // Gateway API HTTPRoute CRD not Established before the registry HTTPRoute apply
)

// 6xxx: trust/hostname (Phase 2)
const (
	CodeTrustCAFail        Code = "CUBE-6001" // local CA creation/load failed (Phase 2)
	CodeTrustOSStoreFail   Code = "CUBE-6002" // OS trust-store install failed (Phase 2)
	CodeTrustOSStoreRevert Code = "CUBE-6003" // OS trust-store uninstall/revert failed (Phase 2)
	CodeTrustCoreDNSFail   Code = "CUBE-6004" // CoreDNS rewrite patch failed or did not roll out (Phase 2)
	CodeTrustCertIssueFail Code = "CUBE-6005" // server certificate issuance failed (Phase 2)
	CodeTrustStateFail     Code = "CUBE-6006" // trust state file corrupt (Phase 2)
)

// 70xx: vendor / air-gap bundle (spec §4.1, Phase 3)
const (
	CodeVendorLockMissing   Code = "CUBE-7001" // cube.lock missing, unreadable, or corrupt (vendor)
	CodeVendorPullFail      Code = "CUBE-7002" // vendor: pull of a pinned pack/image, or writing the bundle itself, failed — produce side (vendor); the consume-side load is CUBE-7006 (bundle is complete-or-error, never partial)
	CodeVendorBundleCorrupt Code = "CUBE-7003" // vendor bundle is unreadable or corrupt (Open)
	CodeVendorIncomplete    Code = "CUBE-7004" // vendor bundle is missing or has corrupt content for a locked pack or image (Verify)
	CodeBundleNoImageLoader Code = "CUBE-7005" // `up --bundle` needs a provider that node-loads images (kind/k3d); `existing` cannot
	CodeBundleImageLoadFail Code = "CUBE-7006" // bundled image load into cluster nodes failed (kind/k3d LoadImages, consume side)
)

// 71xx: exec-plugin discovery (spec §4.4 tier 2, Phase 3)
const (
	CodePluginNotFound    Code = "CUBE-7101" // unknown command and no cube-idp-<name> plugin found on PATH
	CodePluginTrustIO     Code = "CUBE-7102" // plugin trust store (~/.config/cube-idp/trust.json) read/write/hash error
	CodePluginExecFail    Code = "CUBE-7103" // plugin process failed to start/run (not the plugin's own reported exit code)
	CodePluginUntrusted   Code = "CUBE-7104" // plugin refused: unknown or changed sha256, and no interactive confirmation
	CodePluginNameInvalid Code = "CUBE-7105" // plugin name fails the ^[a-z0-9][a-z0-9-]*$ charset guard on `plugin install`/`plugin trust`
	CodePluginNoPlatform  Code = "CUBE-7106" // the official plugin index has no build for this GOOS/GOARCH (P10)
)

// 72xx: sync (Task 10, Task 11)
const (
	CodeSyncNoManifests    Code = "CUBE-7201" // sync dir has no pack.cue and no renderable *.yaml manifests
	CodeSyncWatchSetupFail Code = "CUBE-7202" // `sync --watch` cannot start or attach the filesystem watcher
)

// 73xx: repo (Task 12; CUBE-7304 Phase 5 P7)
const (
	CodeRepoGiteaUnavailable Code = "CUBE-7301" // gitea admin secret missing or port-forward to the gitea pod failed
	CodeRepoGiteaAPIFail     Code = "CUBE-7302" // gitea REST API returned an unexpected status (create/fetch repo)
	CodeRepoDeployFail       Code = "CUBE-7303" // repo created but engine git source registration/apply failed
	// P7 (the gitea guarantee, decision 13): raised at config load.
	CodeRepoDeliveryConfig Code = "CUBE-7304" // delivery: repo needs the gitea pack in spec.packs, and gitea itself cannot be repo-delivered
)

// 8xxx: spoke (Phase 5)
const (
	CodeSpokeProviderUnsupported Code = "CUBE-8001" // spoke cluster.provider invalid for spokes (k3d deferred; existing needs context; duplicate name)
	CodeSpokeBootstrapFailed     Code = "CUBE-8002" // spoke RBAC bootstrap apply failed
	CodeSpokeTokenFailed         Code = "CUBE-8003" // spoke ServiceAccount token issuance failed
	CodeSpokeEnsureFailed        Code = "CUBE-8004" // spoke cluster create/connect failed
	CodeSpokeRegisterFailed      Code = "CUBE-8005" // hub registration secret build/apply failed
	CodeSpokeUnreachable         Code = "CUBE-8006" // spoke hub registration missing, or spoke API server unreachable from this machine
)
