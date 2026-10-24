# NMC³ Console

The website half of the NMC³ stack. The CLI and SDK serve developers. The console serves people who build, run and review agents without writing code, such as business analysts. Every page has a **CLI · SDK** button that shows the same action as code.

```bash
npm install
npm run dev        # http://localhost:3000
```

Add `?theme=light` or `?theme=dark` to force a theme. `?agent=market-scout&tab=activity` opens an agent's drawer directly.

## Pages

| Route | Core need | What it shows |
|---|---|---|
| `/` | Observe | The live run: execution graph, agent lanes, limits, output |
| `/runs`, `/runs/[id]` | Observe | Every run, replayable |
| `/agents` | Build | Agents by team. Click one for **Spec**, **Activity** (what it has done) and **Prompts** |
| `/evaluate` | Evaluate | Test suites, pass rate by version, run a suite |
| `/triggers` | Trigger | API, schedule, webhook, Slack and CLI triggers, with run now and on/off |
| `/budgets` | Budget | Monthly and daily caps, spend by agent and model |
| `/guardrails` | Guardrails | Runtime rules and policies, plus every action they blocked |
| `/prompts` | Prompt observation | Each model turn's system prompt, input, output, tokens, latency and cost |

## Structure

```
src/
  app/(console)/      one folder per page, each with its page.tsx and *.module.css
  components/
    run/              ExecutionGraph, Timeline, LimitMeters, Output, RunList, RunInspector, NewRunDialog
    agent/            AgentDrawer (Spec · Activity · Prompts)
    shell/            Rail, PageHeader
    ui/               Icon, CodeHint, StatePill, Toasts
  config/             what engineers edit: agents, tools, workspace (models, budgets, guardrails, triggers, evals)
  lib/
    simulate.ts       in-browser stand-in for the Go runtime, which follows the v1 contract rules
    layout.ts         automatic graph layout from the agent tree
    store.tsx         runs, settings and UI state
    types.ts          shared shapes, mirroring agents/internal/contracts
```

## Add an agent

1. Add an entry to `src/config/agents.ts`.
2. To put it in a team, list its `id` in an orchestrator's `delegates` and `plan`. Agents in the same `plan` group run in parallel.
3. If it uses a new tool, add the tool to `src/config/tools.ts`. If it needs an icon that isn't registered yet, add it to `components/ui/Icon.tsx`.
4. Optionally give it `sim.steps` to script a demo, including `{ blocked: "tool" | "delegate", target }` to show a guardrail firing.

The graph, lanes, Agents page, drawer, budgets and prompts all pick up the new agent automatically.

## Connecting the real service

Everything is simulated in `lib/store.tsx` and `lib/simulate.ts` today. To go live, replace `startRun` and `cancelLive`, and the history seed, with calls to the Go service: `POST /v1/agent-runs`, `GET /v1/agent-runs/{id}` and `GET /v1/agent-runs/{id}/events?after=N`. Then build each `RunView` from those responses. The components only read `RunView`, so they don't change.
