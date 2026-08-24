// C4 model of cube-idp — source of truth for docs/architecture/.
// Regenerate the rendered SVGs (embedded as images in
// docs/ARCHITECTURE.md §9) from this file via docker, never by hand —
// the c4-architecture skill's canonical pipeline (C4-PlantUML look:
// person icons, system boundary, arrow labels with [technology],
// built-in legend). Run these from this directory; delete the .puml
// intermediates afterwards, so only the DSL and the SVGs live here.
//   docker run --rm -v "$PWD:/usr/local/structurizr" structurizr/structurizr \
//     export -workspace workspace.dsl -format plantuml/c4plantuml
//   docker run --rm -v "$PWD:/data" -w /data plantuml/plantuml -tsvg \
//     structurizr-SystemContext.puml structurizr-Containers.puml \
//     structurizr-Components.puml
//   rm structurizr-*.puml
// Name the .puml files explicitly, as above: passing a glob instead
// (`-tsvg "/data/*.puml"`) fails with "No file found" — the container
// does not expand it. Add a line here when a view is added.
workspace "cube-idp" "Internal developer platform CLI — declarative cube provisioning (v0, post-M9)" {

    model {
        operator = person "Platform Operator" "Declares a cube in cube.yaml and drives it with the cube-idp CLI"

        cubeIdp = softwareSystem "cube-idp" "CLI that provisions and manages the cluster declared in a single Config document; the document is the sole source of truth" {

            cli = container "cube-idp binary" "Single Go binary; cobra CLI with init, create, delete, status, bootstrap, pack render|validate|new (--from / --from-chart) and config validate|show commands" "Go 1.26" {
                mainPkg = component "Entrypoint" "Signal-aware context, delegates to the CLI and exits with the mapped code" "cmd/cube-idp"
                cliPkg = component "CLI edge" "Cobra wiring only: flag mapping, edge composition (init: scaffold-if-absent → load → report; create/delete/status: load → domain operation with injected provisioner factory, SpecValidator type-assert), sole error renderer with exit codes 0/2/1" "internal/cli"
                configDomain = component "Config domain" "Strict load pipeline (decode → Default → Validate), config scaffolding with O_EXCL clobber safety, docker-style name generator; owns CUBE-CFG-* codes" "internal/config"
                apiPkg = component "Config API" "Pure contract: Config types, defaults, validation. No I/O, no logic; hub for the cube-idp.dev/v1alpha1 group" "api/config/v1alpha1"
                clusterDomain = component "Cluster domain" "Provisioner driver seam + optional SpecValidator capability, Init/Delete/Status operations, kubeconfig rebrand/lossless-merge/removal/atomic-write machinery; owns CUBE-CLU-* codes; exported conformance suite" "internal/cluster"
                kindDriver = component "kind driver" "Sole importer of sigs.k8s.io/kind; implements Provisioner + SpecValidator; container-runtime detection deferred to first provisioning call" "internal/cluster/kind"
                kubeDomain = component "Kube domain" "Shared leaf (M6): constructs REST config, discovery, RESTMapper and dynamic client from injected kubeconfig bytes + context name; Ping reachability check; sole constructor of clients (client-go construction confinement); owns CUBE-KUB-* codes" "internal/kube"
                bootstrapDomain = component "Bootstrap domain" "Micro-bootstrap applier (M7): SSA-applies embedded pinned Flux manifests (source-controller + kustomize-controller + helm-controller, M9) + source/sync CRs from spec.engine over injected client-go interfaces, waits the bootstrap kind-set (CRD Established, Deployment/StatefulSet ready, Job complete, Namespace Active), records an inventory (seed of down); SSA hand-rolled on client-go (no new dependency), does not import internal/kube; owns CUBE-BST-* codes" "internal/bootstrap"
                packDomain = component "Pack domain" "Defines, loads, validates and renders packs (M8): pack.cue metadata with a closed #Values definition, raw, kustomize and helm rendering into a RenderPlan{Prerequisites, Objects} — a type: helm pack is thin and renders to a Flux HelmRelease + its HelmRepository/OCIRepository source CR rather than to expanded manifests, RFC 7386 values merge, namespace injection whose scope reads bundled CRDs, hermetic rejection of remote kustomize references, instance identity + dependsOn graph, and the pack new scaffold (including --from-chart, a local Chart.yaml/values.yaml metadata read). Renders, never applies; owns CUBE-PKG-* codes" "internal/pack"
                refLeaf = component "Reference leaf" "Shared infrastructure (M8): one reference grammar resolved to a tree or a single file, explicit schemes only. Local paths and https today; git+https, oci and s3 are recognized and return their own not-implemented codes. Records a pin and enforces containment; owns CUBE-REF-* codes" "internal/ref"
                cubeerrPkg = component "Error machinery" "Coded error shape (code, summary, remediation) and exit-code mapping only — no code catalog" "internal/cubeerr"
            }

            configDoc = container "Config document" "The declared cube (metadata.name + spec.cluster); scaffolded by init when absent, only ever written by the config domain" "cube.yaml (YAML)" {
                tags "File"
            }

            packDir = container "Pack" "A self-contained, versioned directory of platform content: pack.cue (name, version, explicit type, closed #Values) plus its manifests or kustomization — or, for a helm pack, chart coordinates and no chart content at all. Authored by the operator or scaffolded by pack new; read-only to render" "directory (CUE + YAML)" {
                tags "File"
            }

            kubeconfigFile = container "Kubeconfig" "User's kubeconfig; cube-idp merges its cube-idp.dev/<name> context in losslessly and writes atomically" "~/.kube/config or --kubeconfig path" {
                tags "File"
            }
        }

        containerRuntime = softwareSystem "Container Runtime" "Docker / Podman / nerdctl — hosts the kind cluster nodes" {
            tags "External"
        }

        kindCluster = softwareSystem "kind Kubernetes Cluster" "The local cluster provisioned from spec.cluster; name = the cube's metadata.name" {
            tags "External"
        }

        chartSource = softwareSystem "Helm Chart Repository / OCI Registry" "Where a helm pack's chart is published. cube-idp never contacts it: the pack carries only coordinates, and the cluster's helm-controller does the pulling and templating" {
            tags "External"
        }

        refSource = softwareSystem "Remote Reference Source" "Content addressed by an https reference — a values document or a single manifest fetched at render time. git, oci and s3 sources are recognized by the grammar but land with their own milestones" {
            tags "External"
        }

        kubectlTool = softwareSystem "kubectl" "Operator's Kubernetes tooling; uses the cube-branded kubeconfig context" {
            tags "External"
        }

        # People / system relationships
        operator -> cli "Runs init, create, delete, status, bootstrap, pack render|validate|new and config validate|show" "shell"
        operator -> packDir "Authors packs, or scaffolds them with pack new"
        operator -> kubectlTool "Operates the cluster with"
        kubectlTool -> kubeconfigFile "Reads contexts from"
        kubectlTool -> kindCluster "Talks to the API server of" "HTTPS"
        containerRuntime -> kindCluster "Hosts the node containers of"

        # Container-level relationships
        cli -> configDoc "Scaffolds when absent (init), reads and validates" "os file I/O"
        cli -> kubeconfigFile "Merges the cube context in losslessly (create), removes it (delete), inspects it (status); always writes atomically, never unlinks" "os file I/O"
        cli -> packDir "Creates (pack new), reads and renders to stdout (pack render, pack validate)" "os file I/O"
        cli -> refSource "Fetches values documents and external manifests referenced by a pack instance" "HTTPS"
        cli -> containerRuntime "Creates/inspects/deletes kind clusters through" "sigs.k8s.io/kind"
        cli -> kindCluster "Provisions (create) and tears down (delete) from spec.cluster, idempotent by name; exports the kubeconfig of" "sigs.k8s.io/kind"
        cli -> kindCluster "Probes API-server readiness of (status)" "k8s.io/client-go HTTPS"
        cli -> kindCluster "Installs embedded Flux + source/sync wiring into and waits the bootstrap kind-set on (bootstrap)" "k8s.io/client-go HTTPS"
        kindCluster -> chartSource "Pulls and templates the chart named by a rendered HelmRelease (helm-controller) — the delegation a helm pack expresses, and the reason cube-idp never runs Helm" "HTTPS / OCI"

        # Component-level relationships (import direction, strictly left to right)
        mainPkg -> cliPkg "Calls Execute; exits with returned code"
        cliPkg -> apiPkg "References ClusterProvider constants in the provisioner-factory seam"
        cliPkg -> configDomain "Scaffold-if-absent, LoadFile; raises NewNameConflictError on --name mismatch"
        cliPkg -> clusterDomain "Composes Init (create), Delete, Status; type-asserts SpecValidator for config validate"
        cliPkg -> kindDriver "Constructs via injected provisioner factory (composition at the edge only)"
        cliPkg -> kubeDomain "Injects kubeconfig bytes + context name; composes Ping into the status reachability line (M6)"
        cliPkg -> bootstrapDomain "Composes bootstrap (M7): injects dynamic.Interface + meta.RESTMapper (built by kube) and spec.engine; runs Flux install + kind-set wait + inventory"
        cliPkg -> cubeerrPkg "Maps error chain to exit code; renders Coded errors to stderr"
        configDomain -> apiPkg "Strict decode → Default() → Validate()"
        clusterDomain -> apiPkg "Provider constants, API group for the context-name prefix"
        configDomain -> cubeerrPkg "Wraps CUBE-CFG-* errors with"
        clusterDomain -> cubeerrPkg "Wraps CUBE-CLU-* errors with"
        kubeDomain -> cubeerrPkg "Wraps CUBE-KUB-* errors with"
        kubeDomain -> kindCluster "Checks API reachability of, discovers resources on" "k8s.io/client-go HTTPS"
        bootstrapDomain -> apiPkg "Reads the spec.engine sub-struct"
        bootstrapDomain -> cubeerrPkg "Wraps CUBE-BST-* errors with"
        bootstrapDomain -> kindCluster "SSA-applies embedded Flux + source/sync CRs to and waits the bootstrap kind-set on (injected client-go interfaces; never imports internal/kube)" "k8s.io/client-go HTTPS"
        cliPkg -> packDomain "Composes pack render|validate|new: resolves the <ref> positional to a filesystem, loads the pack, and renders either the artifact or one spec.packs instance"
        packDomain -> apiPkg "Reads the spec.packs sub-struct (instances, values, externalManifests, dependsOn)"
        packDomain -> cubeerrPkg "Wraps CUBE-PKG-* errors with"
        packDomain -> refLeaf "Resolves valuesRef, externalManifests refs and pack new --from through (shared-infrastructure leaf, imported directly)"
        refLeaf -> cubeerrPkg "Wraps CUBE-REF-* errors with"
        refLeaf -> refSource "Fetches a single document from" "HTTPS"
        kindDriver -> clusterDomain "Implements Provisioner + SpecValidator (compile-time asserted)"
        kindDriver -> containerRuntime "DetectNodeProvider + cluster create/list/delete" "sigs.k8s.io/kind"
    }

    views {
        systemContext cubeIdp "SystemContext" "cube-idp and its environment" {
            include *
            // Reached only by the cluster, never by cube-idp — `include *`
            // would drop it, and dropping it would hide where a helm pack's
            // chart actually comes from.
            include chartSource
            autoLayout
        }

        container cubeIdp "Containers" "The binary and the content it owns, shares, or reads" {
            include *
            include chartSource
            autoLayout
        }

        component cli "Components" "Package structure — arrows follow the strict import direction" {
            include *
            autoLayout
        }

        styles {
            element "Person" {
                shape Person
                background #08427B
                color #ffffff
            }
            element "Software System" {
                background #1168BD
                color #ffffff
            }
            element "Container" {
                background #438DD5
                color #ffffff
            }
            element "Component" {
                background #85BBF0
                color #000000
            }
            element "File" {
                shape Folder
                background #b8d0e8
                color #000000
            }
            element "External" {
                background #999999
                color #ffffff
            }
        }
    }
}
