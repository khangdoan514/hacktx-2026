from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from app.core.errors import AppError

WidgetType = Literal["metric_number", "capacity_table", "margin_table"]


class SourceRef(BaseModel):
    model_config = ConfigDict(extra="forbid")
    type: Literal["metric_version", "dataset_version", "document_version"]
    id: str = Field(min_length=1)


class WidgetSpec(BaseModel):
    model_config = ConfigDict(extra="forbid")
    type: WidgetType
    metric_version_id: str | None = None
    dataset_version_id: str | None = None
    filters: list[str] = Field(default_factory=list)


class DashboardDefinition(BaseModel):
    model_config = ConfigDict(extra="forbid")
    schema_version: Literal[1]
    title: str = ""
    widgets: list[WidgetSpec] = Field(min_length=1)
    filters: list[str] = Field(default_factory=list)
    source_refs: list[SourceRef] = Field(default_factory=list)


def prepare_definition(raw: dict) -> dict:
    """Validate shape only. Access is checked later against the database."""
    if not isinstance(raw, dict):
        raise AppError(422, "invalid_definition", "Dashboard definition must be an object")
    if "actor_type" in raw:
        raise AppError(422, "forbidden_actor", "Actor identity is assigned by the server")
    provenance = raw.get("provenance")
    if isinstance(provenance, dict) and "actor_type" in provenance:
        raise AppError(422, "forbidden_actor", "Actor identity is assigned by the server")
    payload = {key: value for key, value in raw.items() if key not in {"restrictions", "provenance", "actor_type"}}
    try:
        parsed = DashboardDefinition.model_validate(payload)
    except ValidationError as exc:
        raise AppError(422, "invalid_definition", "Dashboard definition is not valid", {"errors": exc.error_count()}) from exc
    blob = parsed.model_dump_json().lower()
    if any(token in blob for token in ("<script", "javascript:", "eval(", "text/html")):
        raise AppError(422, "unsafe_definition", "Dashboard definitions cannot contain executable content")
    for widget in parsed.widgets:
        if widget.type == "metric_number" and not widget.metric_version_id:
            raise AppError(422, "invalid_definition", "metric_number needs a metric version")
    return parsed.model_dump()


def authorize_sources(conn, project, definition: dict) -> dict:
    """Database membership and source ownership. Client restrictions are not consulted."""
    workspace_id = str(project["workspace_id"])
    metric_ids: list[str] = []
    dataset_ids: list[str] = []
    for widget in definition["widgets"]:
        metric_id = widget.get("metric_version_id")
        if metric_id:
            row = conn.execute(
                """
                SELECT id, workspace_id, state
                FROM app.metric_versions
                WHERE id = %s
                """,
                (metric_id,),
            ).fetchone()
            if not row or str(row["workspace_id"]) != workspace_id or row["state"] != "accepted":
                raise AppError(422, "inaccessible_source", "Metric version is not available in this project")
            metric_ids.append(str(row["id"]))
        dataset_id = widget.get("dataset_version_id")
        if dataset_id:
            row = conn.execute(
                """
                SELECT id, workspace_id, state
                FROM app.dataset_versions
                WHERE id = %s
                """,
                (dataset_id,),
            ).fetchone()
            if not row or str(row["workspace_id"]) != workspace_id or row["state"] != "validated":
                raise AppError(422, "inaccessible_source", "Dataset version is not available in this project")
            dataset_ids.append(str(row["id"]))
    source_refs = [{"type": "metric_version", "id": item} for item in metric_ids]
    source_refs.extend({"type": "dataset_version", "id": item} for item in dataset_ids)
    return {
        "schema_version": 1,
        "project_id": str(project["id"]),
        "workspace_id": workspace_id,
        "source_refs": source_refs,
        "restrictions": ["current_project_membership"],
    }


def human_provenance(actor_id: str, source_refs: list[dict]) -> dict:
    return {
        "actor_type": "human",
        "actor_id": str(actor_id),
        "source_versions": source_refs,
    }
