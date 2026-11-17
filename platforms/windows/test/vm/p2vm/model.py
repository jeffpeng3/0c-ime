"""Small result model shared by the runner and the report writer."""

from dataclasses import dataclass, field
from typing import Any

PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"
NOT_RUN = "NOT RUN"
INFO = "INFO"

STATUSES = (PASS, FAIL, SKIP, NOT_RUN, INFO)


@dataclass
class Check:
    """One acceptance check, in the language used by the report."""

    id: str
    scope: str
    status: str
    detail: str = ""
    evidence: dict[str, Any] = field(default_factory=dict)

    def as_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "id": self.id,
            "scope": self.scope,
            "status": self.status,
        }
        if self.detail:
            out["detail"] = self.detail
        if self.evidence:
            out["evidence"] = self.evidence
        return out

    @property
    def failed(self) -> bool:
        return self.status == FAIL


def overall_status(checks: list[Check], live: bool = True) -> str:
    """Aggregate checks.

    A missing live run is reported as NOT RUN rather than a pass or a product
    failure.  SKIP and INFO never turn the overall run red.
    """
    if any(c.status == FAIL for c in checks):
        return FAIL
    if live and any(c.status == NOT_RUN for c in checks):
        return NOT_RUN
    if not live:
        return NOT_RUN
    return PASS
