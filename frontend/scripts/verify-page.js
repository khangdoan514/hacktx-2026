// Paste into the browser console on a signed-in console page (/dashboard, /agents,
// /budgets, /guardrails, /prompts, /definitions, /tools, /approvals, /org).
// It recomputes what that page should show from the raw API and prints PASS/FAIL per check.
(async () => {
  const token = localStorage.getItem("nmc3_token");
  const h = { Authorization: "Bearer " + token };
  const get = async (p) => (await fetch(p, { headers: h })).json();
  const agent = (p) => get("/agent-api" + p);
  const usd = (n) => (n < 1 ? "$" + n.toFixed(3) : "$" + n.toFixed(2));
  const tok = (n) => (n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(Math.round(n)));
  const rows = [...document.querySelectorAll("table tbody tr")].map((tr) => [...tr.children].map((td) => td.textContent.trim()));
  const text = document.body.innerText;
  const results = [];
  const check = (name, ok, detail) => results.push({ check: name, result: ok ? "PASS" : "FAIL", detail });
  const path = location.pathname;

  const published = async () => (await agent("/definitions")).definitions.filter((d) => d.state === "published");
  const runsWithEvents = async () => {
    const runs = (await agent("/agent-runs")).runs;
    for (const r of runs) r.events = (await agent("/agent-runs/" + r.id + "/events")).events;
    return runs;
  };

  if (path === "/dashboard") {
    const runs = (await agent("/agent-runs")).runs;
    check("row count", rows.length === runs.length, `${rows.length} shown / ${runs.length} api`);
    for (const r of runs) {
      const row = rows.find((x) => x[0] === r.id) || [];
      check(r.id.slice(0, 12), (row[3] || "").startsWith(r.state) && row[4] === tok(r.usage.input_tokens + r.usage.output_tokens) && row[5] === usd(r.usage.cost_usd), row.join(" | "));
    }
  } else if (path === "/budgets") {
    const defs = await published();
    const runs = await runsWithEvents();
    const per = {};
    for (const r of runs) {
      const agentOf = Object.fromEntries(r.invocations.map((i) => [i.id, i.agent_id]));
      for (const e of r.events) if (e.type === "MODEL_SUCCEEDED" && e.data?.usage) {
        const a = agentOf[e.invocation_id] || r.agent_id;
        per[a] = per[a] || { t: 0, c: 0 };
        per[a].t += e.data.usage.input_tokens + e.data.usage.output_tokens;
        per[a].c += e.data.usage.cost_usd;
      }
    }
    for (const d of defs) {
      const row = rows.find((r) => r[0].includes(d.spec.id)) || [];
      const p = per[d.spec.id];
      check(d.spec.id, p ? row[2] === tok(p.t) && row[3] === usd(p.c) : row[2] === "—", row.join(" | "));
    }
  } else if (path === "/guardrails") {
    const defs = await published();
    const runs = await runsWithEvents();
    let denied = 0;
    for (const r of runs) {
      const ev = r.events.filter((e) => e.type === "TOOL_FAILED" || e.type === "DELEGATION_REJECTED");
      denied += ev.length || r.tool_invocations.filter((t) => t.error).length;
    }
    check("denied actions", Number(text.match(/Denied actions\s*\n?\s*(\d+)/)?.[1]) === denied, `api ${denied}`);
    for (const d of defs) {
      const row = rows.find((r) => r[0].includes(d.spec.id)) || [];
      const s = d.spec;
      check(s.id, (row[1] === ((s.allowed_tools || []).join("") || "none")) && (row[2] === ((s.allowed_delegates || []).join("") || "none")) && row[3] === String(s.limits.max_delegations) && row[4] === String(s.limits.max_tool_calls), row.join(" | "));
    }
  } else if (path === "/agents" || path === "/prompts") {
    const defs = await published();
    for (const d of defs) check(d.spec.id + " instructions", text.includes(d.spec.instructions) && text.includes(d.spec.id + "@" + d.spec.version), "");
    if (path === "/agents") {
      const runs = (await agent("/agent-runs")).runs;
      const cards = [...document.querySelectorAll("button")].filter((b) => b.querySelector("small.mono"));
      for (const d of defs) {
        const c = cards.find((b) => b.querySelector("small.mono").textContent.trim() === d.spec.id + "@" + d.spec.version);
        const n = runs.filter((r) => r.invocations.some((i) => i.agent_id === d.spec.id)).length;
        check(d.spec.id + " run count", c && parseInt(c.querySelector("[class*=foot] span b").textContent) === n, "api " + n);
      }
    }
  } else if (path === "/definitions") {
    const defs = (await agent("/definitions")).definitions;
    for (const d of defs) check(d.spec.id + "@" + d.spec.version, text.includes(d.spec.id + "@" + d.spec.version) && text.includes(d.state), d.state);
  } else if (path === "/tools") {
    const org = await get("/api/v1/organization");
    const tools = (await agent("/extension-tools?tenant_id=" + org.id)).tools.filter((t) => t.lifecycle !== "retired");
    check("tenant", [...document.querySelectorAll("input")].some((i) => i.value === org.id), org.id);
    check("rows", rows.length === tools.length, `${rows.length} shown / ${tools.length} api`);
  } else if (path === "/approvals") {
    const waiting = (await agent("/approvals/pending")).approvals.filter((a) => a.state === "WAITING");
    check("waiting rows", rows.length === waiting.length, `${rows.length} shown / ${waiting.length} api`);
  } else if (path === "/org") {
    const org = await get("/api/v1/organization");
    const names = [org.name, ...org.divisions.map((d) => d.name), ...org.divisions.flatMap((d) => d.teams.map((t) => t.name))];
    for (const n of names) check(n, text.includes(n), "");
  } else {
    results.push({ check: "page", result: "SKIP", detail: "open one of the console pages first" });
  }
  console.table(results);
  console.log(results.every((r) => r.result !== "FAIL") ? "ALL PASS" : "SOME FAILED");
})();
