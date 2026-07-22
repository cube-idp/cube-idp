<!-- cube:doc area=diagnostics code=internal/diag,internal/doctor adrs=0029,0030,0031,0040 -->
# Architecture — diagnostics

Governing decisions: ADR-0029 (doctor reports one tri-state row per
registered check), ADR-0030 (typed CUBE diagnostics as the only failure
surface, including bounded waits), ADR-0031 (central append-only diagnostic
code catalog with domain-partitioned ranges), ADR-0040 (diagnostic-code
identifiers are stable product surface).

<!-- cube:section area=diagnostics topic=catalog code=internal/diag -->
## Diagnostic code catalog
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=diagnostics topic=surfaces code=internal/diag -->
## Typed error surfaces
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=diagnostics topic=doctor code=internal/doctor -->
## Doctor checks
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._
