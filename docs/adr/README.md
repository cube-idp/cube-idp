# Architecture Decision Records

Records of architecturally significant decisions for cube-idp: what was decided, why, and
where the code implements it.

ADRs 0002–0039 were reconstructed on 2026-07-20 from the archived planning corpus
(`docs/archive/superpowers/`). Each consolidates several individual decisions that were
first validated against the code — every row in an ADR's *Implementation Status* table
cites the file and line implementing it, so a stale record is falsifiable rather than
merely suspicious.

Status values: `accepted` (in force), `superseded by ADR-NNNN`, `deprecated`.

| # | Decision | Decisions consolidated |
| --- | --- | --- |
| 0001 | [Adopt architecture decision records](0001-adopt-architecture-decision-records.md) | — |
| 0002 | [Pack Format: A Versioned, Data-Only Directory Contract](0002-pack-format-data-only-contract.md) | 11 |
| 0003 | [Pack and File Ref Grammar, Digest Pinning, and Fetch Guards](0003-pack-reference-grammar-and-pinning.md) | 23 |
| 0004 | [Pack Values, Helm Value Merge Order, and extraManifests Layering](0004-pack-values-and-extra-manifests.md) | 11 |
| 0005 | [Pack Dependency Graph: Declaration, Implicit Edges, and Deterministic Delivery Ordering](0005-pack-dependency-graph-and-ordering.md) | 30 |
| 0006 | [Per-Pack Delivery Mode: oci or repo](0006-per-pack-delivery-mode.md) | 5 |
| 0007 | [The GitOps Engine Ships as an Ordinary Pack](0007-engine-as-a-pack.md) | 31 |
| 0008 | [Pack and Plugin Distribution: OCI Artifacts, Catalog Index, and Fallbacks](0008-pack-and-plugin-distribution.md) | 11 |
| 0009 | [Air-Gapped Bundles: Vendoring, Offline Install, and Integrity Verification](0009-air-gapped-bundles-and-integrity.md) | 8 |
| 0010 | [Cluster Provider Set: kind, k3d, existing, Behind a Fixed Interface](0010-cluster-provider-set-and-contract.md) | 10 |
| 0011 | [Provider Config Rendering: Four-Layer Merge and Owned Fields](0011-provider-config-layered-merge.md) | 11 |
| 0012 | [Canonical Gateway Hostname, Host Port, and NodePort Mapping](0012-canonical-gateway-host-and-port-mapping.md) | 8 |
| 0013 | [Spoke Clusters: Declaration, Bootstrap, and Registration-Only Support](0013-spoke-clusters.md) | 25 |
| 0014 | [Teardown Semantics: Cluster Deletion vs Inventory-Driven Cascade](0014-teardown-and-inventory-cascade.md) | 4 |
| 0015 | [In-Cluster zot Registry and Artifact Transport](0015-in-cluster-registry-and-transport.md) | 6 |
| 0016 | [Stateless, Transient Push-Based CLI with No Resident Process](0016-stateless-transient-push-cli.md) | 17 |
| 0017 | [Module Identity, Release Artifacts, and Toolchain Pinning](0017-module-identity-and-release.md) | 5 |
| 0018 | [The GitOpsEngine Interface Seam](0018-gitops-engine-interface-seam.md) | 9 |
| 0019 | [Engine Configuration Is Helm Values, Not a Tuning DSL](0019-engine-values-not-tuning.md) | 10 |
| 0020 | [Engine Self-Management and the Single-Owner Apply Rule](0020-engine-self-management-single-owner.md) | 12 |
| 0021 | [Translating Dependency Order into Engine-Native Ordering Intent](0021-engine-native-ordering-translation.md) | 9 |
| 0022 | [All Rendering Happens In-Process with Contained Dependencies](0022-in-process-rendering.md) | 11 |
| 0023 | [Output Mode Resolution, Color Governance, and the Machine-Readable Contract](0023-progress-mode-and-color-resolution.md) | 11 |
| 0024 | [Plain Output Is Byte-Frozen and Additive-Only](0024-plain-output-byte-freeze.md) | 5 |
| 0025 | [Typed Event Pipeline and Renderer Lifecycle](0025-event-pipeline-and-renderer-lifecycle.md) | 8 |
| 0026 | [Terminal UI Technology: Charm v2, Inline-Only, No Alt Screen](0026-tui-technology-and-scope.md) | 8 |
| 0027 | [Interactive Prompt Doctrine and the Prompt Gate](0027-interactive-prompt-doctrine.md) | 11 |
| 0028 | [CLI Command Surface Freeze and the up-as-Upgrade Path](0028-cli-command-surface-freeze.md) | 4 |
| 0029 | [Doctor Reports One Tri-State Row per Registered Check](0029-doctor-check-reporting.md) | 3 |
| 0030 | [Typed CUBE Diagnostics as the Only Failure Surface, Including Bounded Waits](0030-typed-cube-diagnostics.md) | 9 |
| 0031 | [Central Append-Only Diagnostic Code Catalog with Domain-Partitioned Ranges](0031-diagnostic-code-catalog.md) | 8 |
| 0032 | [cube.yaml Is a KRM Document Authored in Plain YAML](0032-cube-yaml-authoring-surface.md) | 6 |
| 0033 | [cube.lock as a Local KRM Lockfile and Non-Mutating Preview Commands](0033-cube-lock-and-non-mutating-preview.md) | 12 |
| 0034 | [Plugin Trust Consent Flow and External Provenance Verification](0034-plugin-trust-and-provenance.md) | 4 |
| 0035 | [Reproducible Installs: Digest-Pinned Artifacts and Deterministic Republishing](0035-reproducible-digest-pinned-artifacts.md) | 8 |
| 0036 | [Credentials Are Surfaced On Demand, Never Printed Implicitly](0036-credential-surfacing-on-demand.md) | 6 |
| 0037 | [Gateway API as the Routing Surface with a Swappable Gateway Pack](0037-gateway-api-routing-surface.md) | 5 |
| 0038 | [Local CA, TLS at the Gateway, and the OS Trust Store Consent Boundary](0038-local-ca-and-tls-at-the-gateway.md) | 4 |
| 0039 | [Gateway Token Substitution and Render-Derived Gateway Edges](0039-gateway-token-substitution.md) | 4 |

## Conventions

- **Numbering** is sequential and permanent; a superseded ADR keeps its number and gains a
  `superseded by` status rather than being deleted.
- **Code citations** in *Implementation Status* are the contract. If code moves, update the
  citation; if behaviour changes, supersede the ADR.
- **Provenance** in *More Information* points into `docs/archive/superpowers/`, which is
  historical and non-authoritative. Never cite it as authority for new work — cite the ADR.
