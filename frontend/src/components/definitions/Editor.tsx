"use client";

import { useEffect, useState } from "react";
import { useEmployee } from "@/components/shell/ConsoleGate";
import PageHeader from "@/components/shell/PageHeader";
import {
  DefinitionAPIError,
  blankForm,
  definitionsApi,
  formFromVersion,
  isUnmounted,
  specFromForm,
  type DefinitionForm,
  type FieldError,
  type SchemaDraft,
  type Version,
} from "./api";
import { packToolsApi, type PackToolOption } from "./packTools";
import s from "./editor.module.css";

const limitFields: { key: keyof DefinitionForm["limits"]; label: string; step?: string }[] = [
  { key: "max_delegations", label: "Delegations" },
  { key: "max_concurrent_agents", label: "Concurrent agents" },
  { key: "max_tool_calls", label: "Tool calls" },
  { key: "max_model_turns", label: "Model turns" },
  { key: "max_tokens", label: "Tokens" },
  { key: "max_cost_usd", label: "Max cost USD", step: "any" },
  { key: "timeout_seconds", label: "Timeout seconds" },
];

const schemaTypes = ["string", "number", "integer", "boolean", "object", "array"];

export default function Editor() {
  const employee = useEmployee();
  // The service records the signed-in employee; the proxy stamps it, so it is not editable.
  const actor = employee?.display_name || employee?.email || "";
  const [form, setForm] = useState<DefinitionForm>(blankForm);
  const [versions, setVersions] = useState<Version[]>([]);
  const [selected, setSelected] = useState<Version | null>(null);
  const [fields, setFields] = useState<FieldError[]>([]);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const [offline, setOffline] = useState<"unmounted" | "down" | "">("");

  async function reload(next?: Version) {
    const listed = await definitionsApi.list();
    setVersions(listed.definitions);
    if (next) select(next, listed.definitions);
  }

  useEffect(() => {
    let cancelled = false;
    definitionsApi.list().then(
      (listed) => {
        if (!cancelled) setVersions(listed.definitions);
      },
      (error) => {
        if (cancelled) return;
        setOffline(isUnmounted(error) ? "unmounted" : "down");
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  function select(version: Version, list = versions) {
    const stored = list.find((item) => item.spec.id === version.spec.id && item.spec.version === version.spec.version) ?? version;
    setSelected(stored);
    setForm(formFromVersion(stored));
    setFields([]);
    setProblem("");
  }

  function startNew() {
    setSelected(null);
    setForm(blankForm());
    setFields([]);
    setStatus("");
    setProblem("");
  }

  const locked = Boolean(
    selected && selected.spec.id === form.id.trim() && selected.spec.version === form.version.trim() && (selected.state === "published" || selected.state === "retired"),
  );
  const canRevise = Boolean(selected && selected.spec.id === form.id.trim() && form.version.trim() !== selected.spec.version);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setFields([]);
    setProblem("");
    setStatus("");
    try {
      await action();
      setOffline("");
    } catch (error) {
      if (isUnmounted(error)) setOffline("unmounted");
      else if (error instanceof DefinitionAPIError && (error.status === 0 || error.code === "UNAVAILABLE")) setOffline("down");
      if (error instanceof DefinitionAPIError) {
        setFields(error.fields);
        setProblem(error.message);
      } else {
        setProblem("The definition request failed.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <PageHeader
        title="Definitions"
        sub="Draft, test, and publish an agent without editing a file. Test checks tools and delegates. It does not start a model run."
        code={{
          cli: `curl -s -X POST "$API/agent-api/definitions/draft" \\\n  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \\\n  -d @agent.json\n\ncurl -s -X POST "$API/agent-api/definitions/publish" \\\n  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \\\n  -d @agent.json`,
          sdk: `await fetch("/agent-api/definitions/publish", {\n  method: "POST",\n  headers: {\n    "Content-Type": "application/json",\n    Authorization: \`Bearer \${token}\`,\n  },\n  body: JSON.stringify(spec),\n});`,
        }}
      />
      {offline === "unmounted" && (
        <p className={s.banner}>
          This server does not mount the definition handler yet. The routes are <span className="mono">/v1/definitions/draft</span>, <span className="mono">/test</span>, <span className="mono">/publish</span>, <span className="mono">/revise</span>, <span className="mono">/retire</span>, and <span className="mono">/export</span>. The package README has the <span className="mono">Mount</span> snippet for <span className="mono">main.go</span>.
        </p>
      )}
      {offline === "down" && (
        <p className={s.banner}>The definition API is not reachable. Point <span className="mono">AGENT_API_URL</span> at a process that mounts the definitions handler.</p>
      )}
      <div className={s.layout}>
        <section className="card">
          <div className="ch">
            <h2>Versions</h2>
            <div className="right">
              <button type="button" className="btn sm" onClick={startNew}>New</button>
            </div>
          </div>
          {versions.length === 0 ? (
            <p className={s.form}>No definitions yet. Save a draft to start.</p>
          ) : (
            <ul className={s.list}>
              {versions.map((version) => {
                const key = `${version.spec.id}@${version.spec.version}`;
                const pressed = selected?.spec.id === version.spec.id && selected.spec.version === version.spec.version;
                return (
                  <li key={key}>
                    <button type="button" className={s.item} aria-pressed={pressed} onClick={() => select(version)}>
                      <b>{version.spec.name || version.spec.id}</b>
                      <small className="mono">{key}</small>
                      <span className={tagClass(version.state)}>{version.state}</span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </section>

        <section className="card">
          <div className="ch">
            <h2>{form.name.trim() || "New definition"}</h2>
            {selected && <span className={tagClass(selected.state)}>{selected.state}</span>}
            {selected && <span className="mono n" title={selected.content_hash}>{selected.content_hash.slice(0, 12)}</span>}
          </div>
          <div className={s.form}>
            {(problem || fields.length > 0) && (
              <div role="alert">
                {problem && <p className={s.hint}>{problem}</p>}
                {fields.length > 0 && (
                  <ul className={s.errors}>
                    {fields.map((field) => (
                      <li key={`${field.field}:${field.message}`}><span className="mono">{field.field}</span><span>{field.message}</span></li>
                    ))}
                  </ul>
                )}
              </div>
            )}
            {status && <p className={s.ok} role="status">{status}</p>}
            {locked && <p>This version is frozen. Set a new version, then revise. Publish will not overwrite it.</p>}

            <p className={s.field}>Recorded as {actor}</p>
            <div className={s.grid}>
              <Field label="Id" value={form.id} error={messages(fields, "id")} onChange={(id) => setForm({ ...form, id })} />
              <Field label="Version" value={form.version} error={messages(fields, "version")} onChange={(version) => setForm({ ...form, version })} />
              <Field label="Name" value={form.name} error={messages(fields, "name")} onChange={(name) => setForm({ ...form, name })} />
              <div className={s.field}>
                <label>
                  Role
                  <select className="input" value={form.role} onChange={(event) => setForm({ ...form, role: event.target.value as DefinitionForm["role"] })}>
                    <option value="specialist">specialist</option>
                    <option value="orchestrator">orchestrator</option>
                  </select>
                </label>
                <FieldErrorLine fields={messages(fields, "role")} />
              </div>
              <div className={`${s.field} ${s.span}`}>
                <label>
                  Instructions
                  <textarea className="input" value={form.instructions} onChange={(event) => setForm({ ...form, instructions: event.target.value })} />
                </label>
                <FieldErrorLine fields={messages(fields, "instructions")} />
              </div>
              <div className={s.field}>
                <Field label="Tools" value={form.tools} error={messages(fields, "allowed_tools")} onChange={(tools) => setForm({ ...form, tools })} placeholder="catalog.search" />
                <PackToolPicker value={form.tools} onChange={(tools) => setForm({ ...form, tools })} />
              </div>
              <Field label="Delegates" value={form.delegates} error={messages(fields, "allowed_delegates")} onChange={(delegates) => setForm({ ...form, delegates })} placeholder="same version, comma-separated" />
            </div>

            <div className={s.limits}>
              {limitFields.map((limit) => (
                <Field
                  key={limit.key}
                  label={limit.label}
                  value={form.limits[limit.key]}
                  error={messages(fields, `limits.${limit.key}`)}
                  onChange={(value) => setForm({ ...form, limits: { ...form.limits, [limit.key]: value } })}
                  inputMode={limit.step ? "decimal" : "numeric"}
                />
              ))}
            </div>

            <div className={s.schemas}>
              <SchemaFields label="Input" value={form.input} onChange={(input) => setForm({ ...form, input })} />
              <SchemaFields label="Output" value={form.output} onChange={(output) => setForm({ ...form, output })} />
            </div>
            {(messages(fields, "input_schema").length > 0 || messages(fields, "output_schema").length > 0) && (
              <FieldErrorLine fields={[...messages(fields, "input_schema"), ...messages(fields, "output_schema")]} />
            )}

            <div className={s.grid}>
              <Field label="Default model" value={form.defaultModel} error={messages(fields, "model_policy.default_model")} onChange={(defaultModel) => setForm({ ...form, defaultModel })} placeholder="optional" />
              <Field label="Allowed models" value={form.allowedModels} error={messages(fields, "model_policy.allowed_models")} onChange={(allowedModels) => setForm({ ...form, allowedModels })} placeholder="optional, comma-separated" />
            </div>

            <div className={s.actions}>
              <button type="button" className="btn" disabled={busy || locked} onClick={() => run(async () => {
                const saved = await definitionsApi.draft(actor.trim(), specFromForm(form));
                await reload(saved.definition);
                setStatus(`Draft saved for ${saved.definition.spec.id}@${saved.definition.spec.version}.`);
              })}>Save draft</button>
              <button type="button" className="btn" disabled={busy || locked} onClick={() => run(async () => {
                const saved = await definitionsApi.draft(actor.trim(), specFromForm(form));
                const tested = await definitionsApi.test(actor.trim(), saved.definition.spec.id, saved.definition.spec.version);
                await reload(tested.definition);
                setStatus(`Test passed. ${tested.definition.spec.id}@${tested.definition.spec.version} is ready to publish.`);
              })}>Test</button>
              <button type="button" className="btn solid" disabled={busy} onClick={() => run(async () => {
                const published = await definitionsApi.publish(actor.trim(), specFromForm(form));
                await reload(published.definition);
                setStatus(`Published ${published.definition.spec.id}@${published.definition.spec.version}.`);
              })}>Publish</button>
              <button type="button" className="btn" disabled={busy || !canRevise || !selected} onClick={() => run(async () => {
                if (!selected) return;
                const revised = await definitionsApi.revise(actor.trim(), selected.spec.id, selected.spec.version, specFromForm(form));
                await reload(revised.definition);
                setStatus(`Revised ${selected.spec.version} into draft ${revised.definition.spec.version}. The source version was not changed.`);
              })}>Revise</button>
              <button type="button" className="btn" disabled={busy || !selected || selected.state !== "published" || form.id.trim() !== selected.spec.id || form.version.trim() !== selected.spec.version} onClick={() => run(async () => {
                if (!selected) return;
                const retired = await definitionsApi.retire(actor.trim(), selected.spec.id, selected.spec.version);
                await reload(retired.definition);
                setStatus(`Retired ${retired.definition.spec.id}@${retired.definition.spec.version}. Existing run pins still use it.`);
              })}>Retire</button>
            </div>
          </div>
        </section>
      </div>
    </>
  );
}

function Field({
  label, value, onChange, error, placeholder, inputMode,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  error: FieldError[];
  placeholder?: string;
  inputMode?: "numeric" | "decimal";
}) {
  return (
    <div className={s.field}>
      <label>
        {label}
        <input className="input" value={value} placeholder={placeholder} inputMode={inputMode} onChange={(event) => onChange(event.target.value)} />
      </label>
      <FieldErrorLine fields={error} />
    </div>
  );
}

function SchemaFields({ label, value, onChange }: { label: string; value: SchemaDraft; onChange: (value: SchemaDraft) => void }) {
  function update(index: number, patch: Partial<SchemaDraft["properties"][number]>) {
    const properties = value.properties.map((row, i) => (i === index ? { ...row, ...patch } : row));
    onChange({ ...value, properties });
  }
  return (
    <fieldset className={s.schema}>
      <legend>{label} schema</legend>
      {value.properties.map((row, index) => (
        <div className={s.row} key={index}>
          <input className="input" aria-label={`${label} property ${index + 1} name`} value={row.name} placeholder="field" onChange={(event) => update(index, { name: event.target.value })} />
          <select className="input" aria-label={`${label} property ${index + 1} type`} value={row.type} onChange={(event) => update(index, { type: event.target.value })}>
            {schemaTypes.map((type) => <option key={type} value={type}>{type}</option>)}
          </select>
          <button type="button" className="btn sm" onClick={() => onChange({ ...value, properties: value.properties.filter((_, i) => i !== index) })}>Remove</button>
        </div>
      ))}
      <div className={s.actions}>
        <button type="button" className="btn sm" onClick={() => onChange({ ...value, properties: [...value.properties, { name: "", type: "string" }] })}>Add property</button>
        <label className={s.check}>
          <input type="checkbox" checked={value.additional} onChange={(event) => onChange({ ...value, additional: event.target.checked })} />
          Additional properties
        </label>
      </div>
    </fieldset>
  );
}

function FieldErrorLine({ fields }: { fields: FieldError[] }) {
  if (fields.length === 0) return null;
  return <span className={s.hint}>{fields.map((field) => `${field.field}: ${field.message}`).join(" ")}</span>;
}

function messages(fields: FieldError[], key: string) {
  return fields.filter((field) => field.field === key || field.field.startsWith(`${key}.`) || field.field.startsWith(`${key}[`));
}

function tagClass(state: Version["state"]) {
  if (state === "published") return "tag ok";
  if (state === "retired") return "tag warn";
  if (state === "testing") return "tag accent";
  return "tag";
}

// Built-in pack tools as one-click chips. A tool whose pack is not connected
// is dimmed: it can be listed, but calls will report "not connected".
function PackToolPicker({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const [options, setOptions] = useState<PackToolOption[]>([]);
  useEffect(() => {
    packToolsApi.list().then(setOptions).catch(() => setOptions([]));
  }, []);
  if (options.length === 0) return null;
  const selected = new Set(value.split(",").map((part) => part.trim()).filter(Boolean));
  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange([...next].join(", "));
  };
  return (
    <div className={s.chips} role="group" aria-label="Built-in tools">
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          className={`${s.chip} ${selected.has(option.id) ? s.chipOn : ""} ${option.connected ? "" : s.chipOff}`}
          aria-pressed={selected.has(option.id)}
          title={option.connected ? option.description : `${option.description} (${option.pack} is not connected)`}
          onClick={() => toggle(option.id)}
        >
          {option.id}
        </button>
      ))}
    </div>
  );
}
