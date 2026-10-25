// Client for the definitions handler. Next rewrites /agent-api/* to
// ${AGENT_API_URL}/v1/*, so these paths land on /v1/definitions/*.
// The Go process does not mount that handler until the snippet in
// agents/internal/definitions/README.md is added.

export const actorHeader = "X-NMC3-Actor";

export const definitionRoutes = {
  collection: "/agent-api/definitions",
  draft: "/agent-api/definitions/draft",
  test: "/agent-api/definitions/test",
  publish: "/agent-api/definitions/publish",
  revise: "/agent-api/definitions/revise",
  retire: "/agent-api/definitions/retire",
  export: "/agent-api/definitions/export",
} as const;

export type FieldError = { field: string; message: string };

export type Limits = {
  max_delegations: number;
  max_concurrent_agents: number;
  max_tool_calls: number;
  max_model_turns: number;
  max_tokens: number;
  max_cost_usd: number;
  timeout_seconds: number;
};

export type Schema = {
  type?: string;
  additionalProperties?: boolean;
  properties?: Record<string, { type?: string }>;
  required?: string[];
};

export type Spec = {
  id: string;
  version: string;
  name: string;
  role: "orchestrator" | "specialist";
  instructions: string;
  input_schema: Schema;
  output_schema: Schema;
  allowed_tools: string[];
  allowed_delegates: string[];
  limits: Limits;
  model_policy?: { default_model?: string; allowed_models?: string[] };
};

export type Version = {
  spec: Spec;
  schema_version: string;
  content_hash: string;
  state: "draft" | "testing" | "published" | "retired";
  created_by: string;
  created_at: string;
  updated_at: string;
  tested_by?: string;
  tested_at?: string;
  published_by?: string;
  published_at?: string;
  retired_by?: string;
  retired_at?: string;
};

export type SchemaDraft = {
  properties: { name: string; type: string }[];
  additional: boolean;
};

export type DefinitionForm = {
  id: string;
  version: string;
  name: string;
  role: "orchestrator" | "specialist";
  instructions: string;
  tools: string;
  delegates: string;
  limits: Record<keyof Limits, string>;
  input: SchemaDraft;
  output: SchemaDraft;
  defaultModel: string;
  allowedModels: string;
};

const schemaTypes = ["string", "number", "integer", "boolean", "object", "array"];

export function blankSchema(): SchemaDraft {
  return { properties: [], additional: false };
}

export function blankForm(): DefinitionForm {
  return {
    id: "",
    version: "1.0.0",
    name: "",
    role: "specialist",
    instructions: "",
    tools: "",
    delegates: "",
    limits: {
      max_delegations: "1",
      max_concurrent_agents: "1",
      max_tool_calls: "2",
      max_model_turns: "4",
      max_tokens: "4000",
      max_cost_usd: "1",
      timeout_seconds: "30",
    },
    input: blankSchema(),
    output: blankSchema(),
    defaultModel: "",
    allowedModels: "",
  };
}

export function specFromForm(form: DefinitionForm): Spec {
  const spec: Spec = {
    id: form.id.trim(),
    version: form.version.trim(),
    name: form.name.trim(),
    role: form.role,
    instructions: form.instructions,
    input_schema: schemaOf(form.input),
    output_schema: schemaOf(form.output),
    allowed_tools: splitIDs(form.tools),
    allowed_delegates: splitIDs(form.delegates),
    limits: {
      max_delegations: num(form.limits.max_delegations),
      max_concurrent_agents: num(form.limits.max_concurrent_agents),
      max_tool_calls: num(form.limits.max_tool_calls),
      max_model_turns: num(form.limits.max_model_turns),
      max_tokens: num(form.limits.max_tokens),
      max_cost_usd: num(form.limits.max_cost_usd),
      timeout_seconds: num(form.limits.timeout_seconds),
    },
  };
  const policy = policyOf(form);
  if (policy) spec.model_policy = policy;
  return spec;
}

export function formFromVersion(version: Version): DefinitionForm {
  const form = blankForm();
  const spec = version.spec;
  form.id = spec.id;
  form.version = spec.version;
  form.name = spec.name;
  form.role = spec.role === "orchestrator" ? "orchestrator" : "specialist";
  form.instructions = spec.instructions ?? "";
  form.tools = (spec.allowed_tools ?? []).join(", ");
  form.delegates = (spec.allowed_delegates ?? []).join(", ");
  form.limits = {
    max_delegations: String(spec.limits?.max_delegations ?? ""),
    max_concurrent_agents: String(spec.limits?.max_concurrent_agents ?? ""),
    max_tool_calls: String(spec.limits?.max_tool_calls ?? ""),
    max_model_turns: String(spec.limits?.max_model_turns ?? ""),
    max_tokens: String(spec.limits?.max_tokens ?? ""),
    max_cost_usd: String(spec.limits?.max_cost_usd ?? ""),
    timeout_seconds: String(spec.limits?.timeout_seconds ?? ""),
  };
  form.input = schemaDraft(spec.input_schema);
  form.output = schemaDraft(spec.output_schema);
  form.defaultModel = spec.model_policy?.default_model ?? "";
  form.allowedModels = (spec.model_policy?.allowed_models ?? []).join(", ");
  return form;
}

export class DefinitionAPIError extends Error {
  status: number;
  code: string;
  fields: FieldError[];

  constructor(status: number, code: string, message: string, fields: FieldError[] = []) {
    super(message);
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

export function isUnmounted(error: unknown) {
  return error instanceof DefinitionAPIError && error.status === 404 && error.message === "endpoint not found";
}

export const definitionsApi = {
  list: () => send<{ definitions: Version[] }>("GET", definitionRoutes.collection),
  get: (id: string, version: string) => {
    const query = new URLSearchParams({ id, version });
    return send<{ definition: Version }>("GET", `${definitionRoutes.collection}?${query}`);
  },
  draft: (actor: string, spec: Spec) =>
    send<{ definition: Version }>("POST", definitionRoutes.draft, actor, spec),
  test: (actor: string, id: string, version: string) =>
    send<{ definition: Version }>("POST", definitionRoutes.test, actor, { id, version }),
  publish: (actor: string, spec: Spec) =>
    send<{ definition: Version }>("POST", definitionRoutes.publish, actor, spec),
  revise: (actor: string, id: string, fromVersion: string, spec: Spec) =>
    send<{ definition: Version }>("POST", definitionRoutes.revise, actor, { id, from_version: fromVersion, spec }),
  retire: (actor: string, id: string, version: string) =>
    send<{ definition: Version }>("POST", definitionRoutes.retire, actor, { id, version }),
  export: () => send<unknown>("GET", definitionRoutes.export),
};

function schemaOf(draft: SchemaDraft): Schema {
  const properties: Record<string, { type: string }> = {};
  const required: string[] = [];
  const seen = new Set<string>();
  const rows = [...draft.properties].sort((a, b) => a.name.trim().localeCompare(b.name.trim()));
  for (const row of rows) {
    const name = row.name.trim();
    if (!name || seen.has(name) || !schemaTypes.includes(row.type)) continue;
    seen.add(name);
    properties[name] = { type: row.type };
    required.push(name);
  }
  const schema: Schema = { type: "object", additionalProperties: draft.additional };
  if (required.length > 0) {
    schema.properties = properties;
    schema.required = required;
  }
  return schema;
}

function schemaDraft(schema: Schema | undefined): SchemaDraft {
  const properties = Object.entries(schema?.properties ?? {}).map(([name, value]) => ({
    name,
    type: value?.type && schemaTypes.includes(value.type) ? value.type : "string",
  }));
  return { properties, additional: schema?.additionalProperties === true };
}

function policyOf(form: DefinitionForm) {
  const defaultModel = form.defaultModel.trim();
  const allowed = splitIDs(form.allowedModels);
  if (!defaultModel && allowed.length === 0) return undefined;
  return {
    ...(defaultModel ? { default_model: defaultModel } : {}),
    ...(allowed.length ? { allowed_models: allowed } : {}),
  };
}

function splitIDs(value: string) {
  return value.split(",").map((part) => part.trim()).filter(Boolean);
}

function num(value: string) {
  if (value.trim() === "") return 0;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

async function send<T>(method: string, path: string, actor?: string, body?: unknown): Promise<T> {
  const headers = new Headers();
  if (actor) headers.set(actorHeader, actor);
  if (body !== undefined) headers.set("Content-Type", "application/json");
  let response: Response;
  try {
    response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
    });
  } catch {
    throw new DefinitionAPIError(0, "UNAVAILABLE", "The definition API is not reachable.");
  }
  const text = await response.text();
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      throw new DefinitionAPIError(response.status, "UNAVAILABLE", "The definition API did not return JSON.");
    }
  }
  if (!response.ok) {
    const error = parsed && typeof parsed === "object" && "error" in parsed ? (parsed as { error?: { code?: string; message?: string; fields?: FieldError[] } }).error : undefined;
    throw new DefinitionAPIError(response.status, error?.code ?? "UNAVAILABLE", error?.message ?? `Definition API failed (${response.status})`, error?.fields ?? []);
  }
  return parsed as T;
}
