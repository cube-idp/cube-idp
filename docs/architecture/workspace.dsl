// C4 model of cube-idp — source of truth for docs/architecture/.
// Regenerate the rendered assets (SVGs here, mermaid embeds in
// docs/ARCHITECTURE.md) from this file via docker, never by hand:
//   docker run --rm -v "$PWD:/usr/local/structurizr" structurizr/structurizr \
//     export -workspace workspace.dsl -format mermaid
//   docker run --rm -v "$PWD:/usr/local/structurizr" structurizr/structurizr \
//     export -workspace workspace.dsl -format plantuml/c4plantuml
//   docker run --rm -v "$PWD:/data" plantuml/plantuml -tsvg /data/*.puml
workspace "cube-idp" "Internal developer platform CLI — declarative cube provisioning (v0, post-M5)" {

    model {
        operator = person "Platform Operator" "Declares a cube in cube.yaml and drives it with the cube-idp CLI"

        cubeIdp = softwareSystem "cube-idp" "CLI that provisions and manages the cluster declared in a single Config document; the document is the sole source of truth" {

            cli = container "cube-idp binary" "Single Go binary; cobra CLI with init, create, delete, status and config validate|show commands" "Go 1.26" {
                mainPkg = component "Entrypoint" "Signal-aware context, delegates to the CLI and exits with the mapped code" "cmd/cube-idp"
                cliPkg = component "CLI edge" "Cobra wiring only: flag mapping, edge composition (init: scaffold-if-absent → load → report; create/delete/status: load → domain operation with injected provisioner factory, SpecValidator type-assert), sole error renderer with exit codes 0/2/1" "internal/cli"
                configDomain = component "Config domain" "Strict load pipeline (decode → Default → Validate), config scaffolding with O_EXCL clobber safety, docker-style name generator; owns CUBE-CFG-* codes" "internal/config"
                apiPkg = component "Config API" "Pure contract: Config types, defaults, validation. No I/O, no logic; hub for the cube-idp.dev/v1alpha1 group" "api/config/v1alpha1"
                clusterDomain = component "Cluster domain" "Provisioner driver seam + optional SpecValidator capability, Init/Delete/Status operations, kubeconfig rebrand/lossless-merge/removal/atomic-write machinery; owns CUBE-CLU-* codes; exported conformance suite" "internal/cluster"
                kindDriver = component "kind driver" "Sole importer of sigs.k8s.io/kind; implements Provisioner + SpecValidator; container-runtime detection deferred to first provisioning call" "internal/cluster/kind"
                cubeerrPkg = component "Error machinery" "Coded error shape (code, summary, remediation) and exit-code mapping only — no code catalog" "internal/cubeerr"
            }

            configDoc = container "Config document" "The declared cube (metadata.name + spec.cluster); scaffolded by init when absent, only ever written by the config domain" "cube.yaml (YAML)" {
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

        kubectlTool = softwareSystem "kubectl" "Operator's Kubernetes tooling; uses the cube-branded kubeconfig context" {
            tags "External"
        }

        # People / system relationships
        operator -> cli "Runs init, create, delete, status and config validate|show" "shell"
        operator -> kubectlTool "Operates the cluster with"
        kubectlTool -> kubeconfigFile "Reads contexts from"
        kubectlTool -> kindCluster "Talks to the API server of" "HTTPS"
        containerRuntime -> kindCluster "Hosts the node containers of"

        # Container-level relationships
        cli -> configDoc "Scaffolds when absent (init), reads and validates" "os file I/O"
        cli -> kubeconfigFile "Merges the cube context in losslessly (create), removes it (delete), inspects it (status); always writes atomically, never unlinks" "os file I/O"
        cli -> containerRuntime "Creates/inspects/deletes kind clusters through" "sigs.k8s.io/kind"
        cli -> kindCluster "Provisions (create) and tears down (delete) from spec.cluster, idempotent by name; exports the kubeconfig of" "sigs.k8s.io/kind"

        # Component-level relationships (import direction, strictly left to right)
        mainPkg -> cliPkg "Calls Execute; exits with returned code"
        cliPkg -> configDomain "Scaffold-if-absent, LoadFile; raises NewNameConflictError on --name mismatch"
        cliPkg -> clusterDomain "Composes Init (create), Delete, Status; type-asserts SpecValidator for config validate"
        cliPkg -> kindDriver "Constructs via injected provisioner factory (composition at the edge only)"
        cliPkg -> cubeerrPkg "Maps error chain to exit code; renders Coded errors to stderr"
        configDomain -> apiPkg "Strict decode → Default() → Validate()"
        clusterDomain -> apiPkg "Provider constants, API group for the context-name prefix"
        configDomain -> cubeerrPkg "Wraps CUBE-CFG-* errors with"
        clusterDomain -> cubeerrPkg "Wraps CUBE-CLU-* errors with"
        kindDriver -> clusterDomain "Implements Provisioner + SpecValidator (compile-time asserted)"
        kindDriver -> containerRuntime "DetectNodeProvider + cluster create/list/delete" "sigs.k8s.io/kind"
    }

    views {
        systemContext cubeIdp "SystemContext" "cube-idp and its environment" {
            include *
            autoLayout
        }

        container cubeIdp "Containers" "The binary and the two files it owns/shares" {
            include *
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
