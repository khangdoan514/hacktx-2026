# Frontend design

The NMC³ Console in `frontend/` is the website in a CLI, SDK and website stack. The CLI and SDK are for developers. The website is for less technical users such as business analysts. Every page has a **CLI · SDK** button that shows the code equivalent of what it does.

It is organized around the five problems teams hit when they build agents:

| Need | Page |
|---|---|
| Evaluate | `/evaluate`: suites, pass rate by version, run a suite |
| Trigger | `/triggers`: API, schedule, webhook, Slack and CLI |
| Budget | `/budgets`: monthly and daily caps, spend by agent and model |
| Guardrails | `/guardrails`: runtime rules and policies, plus a feed of blocked actions |
| Prompt observation | `/prompts`, plus the **Prompts** tab in the agent drawer |

The live run (`/`) and run replays (`/runs/[id]`) draw execution literally: caller → orchestrator → specialists → tool calls. A lane timeline replaces the text event log. Clicking any agent opens a drawer with its **Spec**, its **Activity** (every past invocation, success rate, tools used and blocked actions) and its **Prompts**.

## Visual language

- Geist and Geist Mono. Neutral surfaces with hairline borders, one blue accent, green for done and red only for blocked or failed.
- The running agent has an animated light around its border, and active edges carry a moving dash.
- Light and dark themes. The design tokens are in `src/app/globals.css`, and component styles are in CSS modules beside each component.
- Minimal text: short labels, details in tooltips and drawers.

## Extending

Agents, tools, budgets, guardrails, triggers and eval suites are declared in `frontend/src/config/`. The graph lays itself out from the agent tree, so adding an agent there makes it appear everywhere. See `frontend/README.md`.

Data is simulated in the browser following the v1 contracts in `agents/internal/contracts`. The `docs/design/relay-console.html` prototype is the earlier single-file version of this design.
