<!-- cube:doc area=engine code=internal/engine,internal/apply,internal/syncer adrs=0007,0018,0019,0020,0021 -->
# Architecture — engine

Governing decisions: ADR-0007 (engine ships as an ordinary pack),
ADR-0018 (GitOpsEngine interface seam), ADR-0019 (engine config is Helm
values), ADR-0020 (engine self-management, single-owner apply),
ADR-0021 (dependency order → engine-native ordering intent).

<!-- cube:section area=engine topic=seam code=internal/engine,internal/engine/contract,internal/engine/factory -->
## Engine interface seam
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=engine topic=flux code=internal/engine/flux -->
## Flux engine
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=engine topic=argocd code=internal/engine/argocd -->
## Argo CD engine
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=engine topic=ordering code=internal/apply,internal/syncer -->
## Ordering and apply
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._
