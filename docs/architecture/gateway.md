<!-- cube:doc area=gateway code=internal/config,internal/pack adrs=0012,0037,0038,0039 -->
# Architecture — gateway

Governing decisions: ADR-0012 (canonical gateway hostname, host port,
NodePort mapping), ADR-0037 (Gateway API routing surface with a swappable
gateway pack), ADR-0038 (local CA, TLS at the gateway, OS trust store
consent), ADR-0039 (gateway token substitution, render-derived edges).

<!-- cube:section area=gateway topic=routing code=internal/pack -->
## Routing surface
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=gateway topic=host-port code=internal/config,internal/cluster -->
## Hostname and port mapping
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=gateway topic=tls code=internal/trust -->
## TLS, local CA, and trust store
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=gateway topic=substitution code=internal/pack -->
## Token substitution
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._
