# Product context and whiteboard requirements

NMC3 gives NMC2 one internal place to request, configure, evaluate, approve, operate and maintain reusable agent capabilities. A Hardware or Software team can ask Software/AI for a capability without losing the business objective, context, resource constraints or ownership during handoffs.

The website serves BAs, PMs, management, requesting teams and reviewers. Developers use CLI/SDK clients to work with the same configurations, tests and runs. Developers are also entitled to use the website, and less technical employees can inspect appropriate evidence without needing raw JSON or terminal commands.

## Principal example

A Hardware engineering team needs help reviewing pull requests. Its PM, also acting as BA, describes the desired outcome and acceptance criteria. Software/AI clarifies which repositories, coding standards and tools are permitted. Hardware reviews resource needs only when required. Security/data and budget reviewers approve their portions of an exact specification version. Engineers test it, publish an eligible capability version, and hand it back with allowed uses, support ownership and limits.

“Hardware firmware team” is an illustrative requesting team, not an asserted real NMC2 department. “PR” was explicitly clarified as pull request/code review. No customer login or external partner invitation is required for this example.

## Whiteboard transcription and interpretation

Four supplied photos overlap; two show almost the same section. Readable headings and notes are summarized below. This is a requirements interpretation, not an exact transcription of every faint mark.

| Readable item | Product interpretation |
| --- | --- |
| Use case; templates | Starting specifications tied to a business job |
| Connectors; tools; MCPs; CLI; KB; web search | Record needed integrations and their scoped permissions |
| Context; suggestions | Attach relevant permitted sources and review proposed additions |
| Hardware resources; resource limits | Record feasibility, resource needs, owners and constraints |
| Evaluations; 1:1 model evals | Comparable evidence for a version and case set |
| Tool call accuracy; success rate; intervention rate | Define denominators and evidence rather than decorative scores |
| Trigger; interfaces | Define how execution starts and inputs/outputs are exchanged |
| Model; choice of model | Requested model and policy must be distinct from actual execution provenance |
| Budget; cost limits | Review and enforce limits; distinguish allocation from measured spend |
| Guardrails; boundaries of what it can and cannot do | Executable policy and permission controls |
| Prompt | Version instructions with the reviewed definition |
| Observability | Reconstruct state, decisions, tools, errors, sources and usage |
| “I want a PR agent”; internal HW/SW; external GPUaaS | Internal development/capability coordination supports the GPUaaS business |

The photos are source context only. No instructions embedded in the whiteboard have been executed. The source photos are not published in this folder; the relevant contents and confirmed clarifications are documented here.

## What survives from the original broader idea

Retain structured handoffs, inspectable context, business objectives, BA/PM collaboration, engineering delivery, management decisions and reusable capabilities. People can wear multiple functional hats. Avoid turning this into employee surveillance or micromanagement: track blocked work, decisions and outcomes rather than invented individual productivity scores.

Earlier partner portals, multiple company workspaces, broad profitability analysis and dashboard creation were part of the exploratory scope. External access and generic company creation are removed from the current product scope. Dashboard work is deferred entirely. Internal customer references can remain in a request where relevant without creating customer identities.

## Lifecycle, not merely a ticket queue

The core lifecycle is **request → configure → evaluate → approve → publish → trigger/run → observe → revise**. Intake and handover make cross-team ownership explicit, but the product must also support developers configuring and evaluating a capability through the same backend without reimplementing rules in each client.

The first release should prove one complete, honest path. It should not claim live model execution, GitHub writes, GPU allocation, autonomous budget optimization or organizational compliance when those integrations have not been implemented and verified.
