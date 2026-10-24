from datetime import date, datetime
from decimal import Decimal


def _as_date(value) -> date | None:
    if value is None or value == "":
        return None
    if isinstance(value, datetime):
        return value.date()
    if isinstance(value, date):
        return value
    return date.fromisoformat(str(value)[:10])


def gross_margin(revenue: Decimal | None, cost: Decimal | None) -> tuple[Decimal | None, str | None]:
    if cost is None:
        return None, "missing_cost"
    if revenue is None:
        return None, "missing_revenue"
    if revenue == 0:
        return None, "zero_revenue"
    return (revenue - cost) / revenue, None


def capacity_margin(rows: list[dict], parameters: dict) -> dict:
    """Registered gross-margin and capacity check.

    Pool figures (available and committed) are repeated on each partner row.
    The pool uses the max of each figure so repeated rows are not summed.
    Missing cost is excluded. It is never treated as zero.
    """
    target = Decimal(str(parameters["target_margin"]))
    period = str(parameters["forecast_period"])
    exclude_before = _as_date(parameters.get("exclude_contract_end_before"))

    partners: list[dict] = []
    excluded: list[dict] = []
    pools: dict[str, dict[str, Decimal]] = {}
    requested = Decimal("0")

    seen: set[tuple] = set()
    for row in rows:
        if row.get("quarantined"):
            excluded.append(
                {
                    "partner_id": row.get("partner_ref") or row.get("partner_id"),
                    "reason": row.get("quarantine_reason") or "quarantined",
                }
            )
            continue
        values = row["values"] if "values" in row else row
        key = (
            values.get("partner_id"),
            values.get("contract_id"),
            values.get("period"),
            values.get("resource"),
        )
        if key in seen:
            excluded.append({"partner_id": values.get("partner_id"), "reason": "duplicate_key"})
            continue
        seen.add(key)
        if values.get("period") != period:
            excluded.append({"partner_id": values.get("partner_id"), "reason": "period_mismatch"})
            continue
        contract_end = _as_date(values.get("contract_end"))
        if exclude_before and contract_end and contract_end < exclude_before:
            excluded.append({"partner_id": values.get("partner_id"), "reason": "contract_expiring"})
            continue
        revenue = _decimal(values.get("revenue"))
        cost = _decimal(values.get("included_cost"))
        margin, reason = gross_margin(revenue, cost)
        extra = _decimal(values.get("extra_allocation")) or Decimal("0")
        resource = str(values.get("resource") or "default")
        pool = pools.setdefault(
            resource,
            {"available": Decimal("0"), "committed": Decimal("0")},
        )
        available = _decimal(values.get("available_capacity"))
        committed = _decimal(values.get("committed_capacity"))
        if available is not None and available > pool["available"]:
            pool["available"] = available
        if committed is not None and committed > pool["committed"]:
            pool["committed"] = committed
        eligible = reason is None and margin is not None and margin >= target
        entry = {
            "partner_id": values.get("partner_id"),
            "contract_id": values.get("contract_id"),
            "resource": resource,
            "margin": _text(margin),
            "eligible": eligible,
            "extra_allocation": _text(extra),
            "reason": reason if not eligible else None,
        }
        if reason == "missing_cost":
            entry["reason"] = "missing_cost"
        elif reason == "zero_revenue":
            entry["reason"] = "zero_revenue"
        elif margin is not None and margin < target:
            entry["reason"] = "below_target_margin"
        partners.append(entry)
        if eligible:
            requested += extra

    remaining = Decimal("0")
    for pool in pools.values():
        gap = pool["available"] - pool["committed"]
        if gap > 0:
            remaining += gap
    return {
        "formula": "gross_margin",
        "definition": "(revenue - included_cost) / revenue",
        "target_margin": _text(target),
        "forecast_period": period,
        "partners": partners,
        "excluded": excluded,
        "remaining_capacity": _text(remaining),
        "requested_allocation": _text(requested),
        "capacity_shortfall": requested > remaining,
    }


def _decimal(value) -> Decimal | None:
    if value is None or value == "":
        return None
    return Decimal(str(value))


def _text(value: Decimal | None) -> str | None:
    if value is None:
        return None
    return format(value, "f")
