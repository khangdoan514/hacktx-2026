# FRONTEND.md — NMC³ Console design spec

This document specifies the NMC³ Console well enough that another engineer or AI agent can rebuild it and get the same result.

- **Reference implementation:** `docs/design/relay-console.html`. It is one self-contained file. When this document and that file disagree, **the file wins**, so send both.
- **Status:** design prototype. All data is simulated in the browser. Porting notes are in §12.

---

## 1. Product and design brief

**Challenge (HackTX)**:

> Building an AI agent often involves repeating the same setup: connecting tools, configuring models, writing instructions, and adding monitoring. How can a platform make that setup reusable so people can focus on what their agent should accomplish? Design and prototype a platform that lets users quickly create AI agents for enterprise grade use.

**NMC³** is that platform, running on **NMC²** compute (https://nmc2.com, an HPC/GPU cloud with an HPC site in Spartanburg, SC). Always write it with superscripts: NMC³, NMC².

**The idea**: setup is done **once** as shared *building blocks* in four kinds:

- **Tools**: connectors such as Web search (SerpAPI), Slack, Drive and the data warehouse.
- **Models**: hosted on NMC².
- **Instructions**: policies and playbooks.
- **Monitoring**: tracing, PII redaction, budgets, approvals, evals and SLOs.

Workspace defaults (model, base policies, core monitors) are inherited by every agent. To create an agent, the user describes its goal or picks a template, and the platform assembles the agent from blocks: inherited defaults plus suggestions matched to the goal. One click deploys it.

The overview also shows **live runs** and **decisions**, which is the monitoring layer. The flagship example agent is the **Idea Consultant**, which searches the web, generates ideas on NMC² GPUs, filters them for novelty, has critics debate them, and ranks the result.

**SerpAPI** powers the Idea Consultant's **Scout** step (Google Search, Scholar, Patents, Trends, News). It must be **visible where it's used**: in the Scout column of the river, in live captions, in decisions, in the agent setup card, in one KPI and in the sidebar quota. It should not be a separate stats panel.

**Keep the overview to four things**: the header with Create agent, a 4-number strip, Live runs, and Decisions next to the agent setup card. The block library, the deployed-agents table and compute details belong on their own pages, not the overview.

**Owner's constraints**

| Do | Don't |
|---|---|
| Left sidebar | Top-nav-only layout |
| Several concurrent flows, with visible decisions | One linear pipeline |
| Light **and** dark theme, plus a toggle | Single theme |
| A creative but professional flow visualization (the **idea river**) | Toy-like 3D objects, orbiting cameras, gimmicks |
| A professional, engineered look | Anything that looks "vibe-coded": glass on every card, glows everywhere, many accent colors, chips and icons on everything, a decorative hero object |
| Platform metrics: agents, time to deploy, runs, SerpAPI searches | Financial-services look; too many panels at once; SerpAPI either hidden or given its own stats panel |
| Compact KPIs in one strip | Big hero metric cards |

**Design direction: NMC² language**

- Black and white with very high contrast.
- Hairline dividers instead of shadows and blur.
- Sharp geometry and flat surfaces.
- One electric blue accent: `#2b9cfc` from nmc2.com.
- One "prism light" moment: a soft spectral glow cut by a hard polygon edge, placed only in the flow stage.
- Light film grain over the page.
- Glass (backdrop blur) is used **only** on the sticky top bar. Overlays are solid.
- **The page's one bold element is the Live runs river.** Everything else stays quiet.

---

## 2. Dependencies

```html
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@300;400;500;600&family=IBM+Plex+Mono:wght@400;500&display=swap">
<script src="https://cdn.jsdelivr.net/npm/lucide@0.469.0/dist/umd/lucide.min.js"></script>
```

- **IBM Plex Sans** for UI and **IBM Plex Mono** for data, IDs, times and status text. The engineering feel suits HPC.
- **Lucide icons:** write `<i data-lucide="name">` and call `lucide.createIcons()` after inserting HTML. Icons are 16px with stroke 1.5. Use them sparingly: only in nav, buttons and agent rows.
- **No 3D library.** The flow visualization is hand-drawn on a 2D `<canvas>`.

---

## 3. Tokens

Put the light values on bare `:root`. Redefine them in `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) {…} }` and again in `:root[data-theme="dark"] {…}`. Both dark blocks set `color-scheme: dark`.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--bg` | `#f4f4f2` | `#050506` | Page and sidebar background |
| `--surface` | `#ffffff` | `#0b0b0d` | Panels, inputs |
| `--surface-2` | `#f8f8f6` | `#0f0f12` | Nested surfaces |
| `--line` | `#e4e4e0` | `#1c1c20` | All hairlines and panel borders |
| `--line-2` | `#d4d4cf` | `#2a2a30` | Button borders, stronger rules |
| `--ink` / `--ink-2` / `--ink-3` | `#0b0b0c` / `#3f4045` / `#8a8b90` | `#f4f4f5` / `#b4b5bb` / `#6f7077` | Text: primary, body, muted |
| `--hover` | `rgba(11,11,12,.035)` | `rgba(255,255,255,.035)` | Row and button hover |
| `--solid` / `--solid-fg` | `#0b0b0c` / `#fff` | `#fff` / `#050506` | Primary button, avatar, toast (inverts) |
| `--accent` | `#0a6fd6` | `#2b9cfc` | The one accent (NMC² blue) |
| `--accent-soft` | `rgba(10,111,214,.09)` | `rgba(43,156,252,.13)` | Focus ring, new-row flash |
| `--f1` `--f2` `--f3` | `#0a6fd6` `#0b9e8f` `#6a4fe0` | `#2b9cfc` `#4aeadc` `#b7a8ff` | Flow identities (blue, NMC² cyan, lavender), used only to say "this belongs to flow X" |
| `--ok` / `--warn` / `--err` | `#00965f` / `#e2620c` / `#d92d20` | `#00d084` / `#ff7a1a` / `#ff5a4e` | Status. NMC² green and orange |
| `--glass` | `rgba(244,244,242,.74)` | `rgba(5,5,6,.72)` | Top bar background (with `blur(16px) saturate(160%)`) |
| `--stage-bg` | `#fbfbfa` | `#070709` | River canvas background |
| `--shard-o` | `.4` | `.85` | Prism-light strength |
| `--grain-o` | `.035` | `.06` | Film-grain opacity |
| `--pop` | `0 30px 80px -24px rgba(0,0,0,.28), 0 0 0 1px var(--line)` | the same with `.8` alpha | Dialog, toast, drawer |
| `--r` | `12px` | | Panel radius. Buttons use 8px, chips 6–7px |
| `--ease` | `cubic-bezier(0.16, 1, 0.3, 1)` | | All motion |

**Type scale (IBM Plex)**

| Element | Style |
|---|---|
| `h1` | 300 weight, `clamp(36px, 4.2vw, 54px)`, line-height 1, tracking −.035em |
| Panel title | 15px / 500 |
| Panel caption | 13px, `--ink-3` |
| Body | 14px / 1.5 |
| KPI value | 300 weight, 32px, tracking −.03em, tabular numbers, unit in 14px muted |
| Mono labels | 11–12px Plex Mono: eyebrow, times, flow codes, status text, counts |
| Scores | Mono 18px |

Don't use uppercase labels or letter-spacing on body text.

**Flow code chip** (`.code`): mono 11.5px text in `--fc` (the lane color) after a 6px square rotated 45° (a small diamond) in the same color. Flows are identified **everywhere** by codes like `F-1041`.

---

## 4. Global effects

- **Film grain**: `body::after`, fixed, z-index 200, `pointer-events:none`, `opacity: var(--grain-o)`, `mix-blend-mode: overlay`. Background is an inline SVG `feTurbulence` (fractalNoise, baseFrequency .9, 2 octaves) on a 160px tile.
- **Brand mark**: a 30px rounded square (radius 7) in `--solid` with "N³" in 11.5px/600. Its top-right corner is a 15px triangle (`clip-path: polygon(0 0,100% 0,100% 100%)`) filled with the NMC² spectrum, `linear-gradient(135deg,#ff6900,#c8e06a 25%,#4aeadc 50%,#2b9cfc 75%,#8f7cf6)`.
- **Status dot**: 6px. Classes `ok` / `run` (accent) / `warn` / `idle` (opacity .45). `.live` adds an expanding ring (`::after`, 2.2s).
- **Entrance**: `.rise` animates from `translateY(10px)` and opacity .25 over .9s, staggered with `--d` from 0 to 340ms. Content is never fully invisible.
- **Theme toggle**: a moon/sun button in the top bar. It stores the choice in `localStorage["nmc3-theme"]`, mirrors the effective theme in `<html data-eff>`, and switches with a circular reveal from the button via the View Transitions API (650ms). Shortcut ⌘/Ctrl+Shift+L.
- **Reduced motion**: every animation and transition becomes about 0ms. The river redraws every 500ms without particles.

---

## 5. Layout

```
.app  grid: 232px | 1fr
├─ aside.side          flat, --bg, right hairline, sticky 100vh
├─ .scrim              (mobile drawer)
└─ .main
   ├─ header.top       sticky 56px glass bar, bottom hairline
   └─ .page            max-width 1440, padding 36px 28px 64px, column gap 20
      ├─ section.head  2-col grid: title block | "Create an agent" form (320–520px)
      ├─ section.kpis  one bordered strip, 4 cells
      ├─ section.panel Live runs (idea river + Steps board)
      └─ .row (12-col, gap 20): Decisions c7 | Idea Consultant setup c5
dialog#builder        agent builder (§7.3)
```

**Panel** (`.panel`): `--surface` background, 1px `--line` border, radius 12, no shadow.

- Header `.ph`: min-height 60, padding 16px 20px, bottom hairline. Contents: `h2` title, `.cap` caption, and controls in `.right`.
- Tabs (`.tabs button`): 28px tall, muted text. The pressed tab gets `--hover` fill and an inset 1px `--line` ring.

**Responsive**

| Width | Changes |
|---|---|
| ≤ 1280 | KPI strip shows 3 cells. Decisions and the setup card each go full width. |
| ≤ 960 | Sidebar becomes a drawer (`translateX(-102%)`, slides in, with a scrim). Escape closes it. The hamburger appears, the header stacks, and search collapses to an icon |
| ≤ 640 | Page gutter 16px. KPIs in 2 columns. All panels full width. Legend and breadcrumbs hidden |

### 5.1 Sidebar

- **Brand**: the mark, then "NMC³" (15px/600) with "Agent platform on NMC²" (11.5px muted).
- **Workspace button**: 36px, 1px line border, a mono "HX" tile, "HackTX workspace" and a `chevrons-up-down` icon.
- **Nav links**: 34px tall, 13.5px, radius 7. The current link has `--surface` fill, a 1px line ring, weight 500, and a 2px accent bar at the far left edge. Counts are right-aligned mono 11.5px muted.
  - Overview `layout-grid`
  - Agents `bot` (12, live)
  - Runs `workflow` (3, live)
  - Decisions `git-fork`: "1 waiting" in warn, shown only while a run waits
  - Building blocks `blocks` (24)
  - Group label "Infrastructure", followed by: NMC² compute `server`, SerpAPI `search`, Settings `settings-2`.
- **Footer**: "NMC² · Spartanburg {GPU %}" and "SerpAPI searches {n} / 5,000", both live. Each has a 2px `--ink` bar.

### 5.2 Top bar

- Contents: menu button (mobile), breadcrumbs "HackTX workspace / **Overview**", search (300px, 1px line border, "Search flows, ideas, agents", `⌘K` kbd), theme toggle, bell, and a 30px round `--ink` avatar reading "KD" in mono.
- All icon buttons are 34px and transparent, with a `--hover` fill on hover.
- **⌘K** opens a solid command palette (`<dialog>`, radius 12, `--pop`). Groups:
  - **Actions**: Start a new brief, Adjust novelty threshold, Switch theme.
  - **Flows**: one per lane; choosing one selects that lane.
  - **Agents**: the first four.
  - Navigate with the arrow keys and Enter.

### 5.3 Header

- **Left column**:
  - Mono eyebrow: live dot + "Live" · "Fri 24 Oct · 08:14 CT" · "NMC² us-southeast".
  - `h1` "Agents".
  - Lede (15px): "**12** agents in production · **3** runs in flight." plus either "Agents made **1,092** decisions today." or, while a run waits, a warn link (nowrap) "**F-1042 is waiting on your decision**."
- **Right column**: a `<form>` labeled "Create an agent".
  - A bordered input row with a "Build agent →" solid button. The placeholder types through: "Triage support tickets and draft replies in our tone", "Review vendor contracts and flag risky clauses", "Answer metrics questions from our data warehouse", "Watch status pages and page on-call when latency spikes".
  - **Template chips**: "Start from" followed by Idea consultant · Support copilot · Contract reviewer · Data analyst. Each chip is 26px tall with a 1px line border and radius 6, and opens the builder with its goal.
  - Submitting opens the builder (§7.3).

### 5.4 KPI strip

- One bordered container split into 5 cells by 1px vertical hairlines (no separate cards).
- Each cell: label (12.5px muted), value (32px/300), and a footer row with a mono caption on the left and a 72×20 sparkline on the right.
- Sparkline: 1.2px `--ink-3` line, a 2.2px `--ink` end dot, and a draw-on animation over 1.4s.

| Label | Value | Caption | Sparkline data |
|---|---|---|---|
| Agents in production | `12` (live, +1 per deploy) | +2 this week (ok) | 6,7,7,8,9,10,10,12 |
| Median time to deploy | `4`m`10`s | vs ~2 days by hand | 9,8.1,7,6.2,5.5,5,4.6,4.2 |
| Runs in flight | `3` + "/ 6 slots" (live) | 1,284 today | 2,3,3,2,3,4,3,3 |
| Searches via SerpAPI | `1,846` (live, sum of engine counts) | of 5,000 today | 180,260,240,310,350,330,390,420 |

The strip has 4 equal cells.

---

## 6. Live runs: the idea river

**Concept.** Each brief is a ribbon flowing left to right through the 8 steps.

- **Width is data.** The ribbon's half-width at a step is `2.2 + sqrt(n / 64) × maxHW`, where n is the number of things alive there:

  | Step | brief | scout | map | ideate | novelty | consult | decide | deliver |
  |---|---|---|---|---|---|---|---|---|
  | n | 1 | results | clusters × 2 | ideas | kept | kept | 3 | 3 |

  So it swells while Scout finds sources, pinches while Map clusters them, swells again while Ideate generates, and pinches at the Novelty and Decide gates.
- **The ribbon's tip is the current step.**
- **Particles are individual ideas** flowing inside the ribbon. At a gate, rejected ideas visibly drop out and fall away.
- **Decisions are visible.** A sent-back flow retracts its tip and draws an orange loop back to Ideate. A flow waiting on the user pulses orange at the Decide gate.

The look is a precise, Sankey-like data drawing with no decorative objects.

**Header**:

- Title "Live runs", caption "Idea Consultant · band width is how many ideas are alive at each step".
- Legend:
  - "Ideas in flight": an 18×8 rounded bar with a transparent → accent gradient and an accent hairline.
  - "Decision gate": a 1.5px dashed vertical tick.
  - "Pruned": two small grey dots, offset.
  - "Sent back": a dashed warn line.
- Tabs **River / Steps**, saved in `localStorage["nmc3-flow-view"]`. River is the default.

### 6.1 Container and layers

`.river` is 480px tall with `--stage-bg`. Layers, back to front:

1. `.prism` (z 0): the NMC² prism shard in the top-right corner, `right:-3%; top:-40%`, 28% × 80%, opacity `--shard-o × .35`.
   - `clip-path: polygon(30% 6%, 100% 20%, 92% 56%, 56% 96%)`.
   - Its inner `<i>` (`inset:-25%`, `blur(44px)`) stacks radial gradients `#ecd8ff`, `#a6dcff`, `#2b9cfc`, `#3b6dfc`, and drifts with a hue-rotate over 28s.
2. `<canvas>` (z 1), full size, sized for device pixel ratio (max 2).
3. `.rv-labels` (z 2): an HTML label layer with `pointer-events:none`.
4. `.river-foot`: mono 11.5px. Left: live dot + "Streaming from NMC² · 64 GPUs". Right: "Click a flow to follow its decisions".

### 6.2 Geometry (recompute on resize)

```
LW (label column) = 220px (140px when width ≤ 900); CSS var --lw
TOP = 70, BOT = 40
x0 = LW + 20; colW = (W − x0 − 24) / 8; laneH = (H − TOP − BOT) / 3
maxHW = min(30, laneH × .23)
lane center   yc(i) = TOP + laneH × (i + .5)
column start  colX(j) = x0 + j × colW
gate x        gateX(j) = colX(j) + .22 × colW   (j = 4 Novelty, 6 Decide)
width at x: c = floor((x − x0)/colW), loc = fraction; w = lerp(hw[c−1], hw[c], smoothstep((loc − .08) / .32))
```

**Per-step width while running**: `lerp(final[j−1], final[j], easeOutCubic(p))`, so the ribbon visibly grows or shrinks during the step. Done steps use `final[j]`. Steps not yet reached aren't drawn.

**Tip position** (in step units, 0–8):

- running: `stage + min(p, .98)`
- waiting: `stage + .3` (parked at the gate)
- delivered: `8`

The drawn tip eases toward its target by `min(1, dt × 3.2)` per frame. That's what makes a sent-back flow retract smoothly from Novelty to Ideate.

- On first paint, the tip **starts at its target** (no animation from zero).
- When a lane is recycled (new flow code), the tip resets to 0.

### 6.3 Drawing order per frame

1. **Grid**:
   - A 1px `--line` vertical rule at LW.
   - Horizontal `--line` rules between lanes.
   - Step column dividers at `colX(1..7)` in `--line-2` at 55%.
   - Gate lines at `gateX(4)` and `gateX(6)`: dashed `[3, 4]` in `--ink-3` at 55%, spanning all lanes.
2. **Hover/selected lane wash**: lane color at .03 (hover) or .05 (selected) over that lane's rectangle.
3. **Sent-back loop** (while the 2D board's loop element has `.on` or `.was`):
   - A cubic curve from (gateX(4), top of the ribbon) up to `yc − .46 × laneH`, then down onto Ideate (`colX(3) + colW/2`).
   - Dashed `[4, 5]` with an animated dash offset, 1.5px, warn color at .95 (on) or .3 (was).
   - While on: 3 warn dots run along the curve, and the HTML tag "Sent back · not novel enough" shows at the arc's apex.
4. **Ribbon**: sampled every 3px from x0 to the tip.
   - Ends are rounded: a circular taper over 18px at the start and 14px at the tip.
   - Fill: horizontal gradient from lane color at .03, to .16 (dark) or .11 (light) until 120px before the tip, to .34 (dark) or .24 (light) at the tip.
   - Top and bottom edges: 1px strokes fading from lane color at .05 to .75 at the tip.
5. **Particles**: 70 per lane.
   - Each has: x, a lateral position v ∈ [−1, 1], speed 34–84 px/s, radius .9–2.2, phase, and two prune rolls `pr` and `pr2` ∈ [0, 1).
   - Position: `y = yc + v × w(x) × .78 + sin(2.2t + phase) × 1.1`. Fade in over the first 30px and out over the last 24px before the tip.
   - When a particle reaches the tip, it respawns near x0.
   - **Pruning**: when a particle crosses `gateX(4)` and `pr > keep`, it starts falling. While Novelty runs, `keep = 1 − (1 − kept/ideas) × p`; once done, `keep = kept/ideas`. Decide does the same with `pr2` and `3/kept`.
   - A falling particle turns `--ink-3`, accelerates downward (vy from −12, gravity 140 px/s²), drifts right at 10 px/s, fades over 1.1s, then respawns.
6. **Tip marker**: a solid dot (r 4.2) plus an expanding ring (r 5→19, fading, 1.25s cycle).
   - Color: lane color, **warn** while waiting (faster pulse), or **ok** when delivered (r 6 with a check mark drawn inside).
7. **Gate highlight**: while a lane's gate is running, a 2px vertical segment in the lane color spans `yc ± (maxHW + 10)` at the gate. While waiting, it's warn, pulsing between opacity .2 and .9.

When another lane is selected, multiply every alpha in this lane by .2.

### 6.4 HTML labels

| Label | Position | Content |
|---|---|---|
| Step headers ×8 | Centered on each column, top 16px | Mono "01"–"08", step name (12.5px/500), sub (11px muted). Gate headers get a small accent hexagon |
| Lane label ×3 | x 20px, centered on `yc(i)`, width `--lw − 40px` | Flow code chip, title (13.5px/500), sub line `{client} · N/8`, **"Waiting on your call"** (warn) or "Delivered", and a 2px meter in the lane color showing tip / 8 |
| Tip chip | Above the ribbon at the tip (x clamped to [x0 + 70, W − 90]), anchored bottom-center | Surface background, 1px line ring and shadow, mono 11.5px/500, with a lane-color dot. Text `{agent} · {live status}` (e.g. "Novelty Judge · scoring"). While waiting: warn styling, text "Your call · break a tie", clickable (scrolls to Decisions and re-flashes the escalation item). Delivered: ok dot, "Delivered to {client}" |
| Step captions | Under the ribbon at `yc + maxHW + 14`, centered per column; only for **done** steps 2–8 | Mono 11px muted with a bold ink-2 value: "42 results", "6 clusters", "24 ideas", "**7 kept** · 17 pruned", "2 rounds", "**top 3**", "brief ready" |

Dimmed lanes put all their labels at opacity .25.

### 6.5 Interaction and performance

- **Hover**: picks the lane under the pointer (by y). The cursor becomes a pointer and the lane gets a wash.
- **Click**: `selectLane(i)` toggles. It's shared with the Steps view and the Decisions filter.
- **Rendering**: `requestAnimationFrame` runs only while the canvas is on screen, the tab is visible and the River tab is active.
- **Theme**: palette tokens are re-read when `<html data-eff>` changes.

### 6.6 Steps view (alternative and fallback)

A clean 2D board, 980px minimum width:

- **Columns**: step headers.
- **Lanes**: 92px rows with labels in a 184px left area. Each label shows the flow code, title, client and a 2px progress bar.
- **Nodes**: 48px tall. Steps have rounded corners; gates are hexagons (`clip-path: polygon(14px 0, calc(100% - 14px) 0, 100% 50%, calc(100% - 14px) 100%, 14px 100%, 0 50%)`). The 1px border shell color follows the state: idle = line-2 at .4 opacity, done = lane at 50%, running = lane plus a 4px halo, waiting = blinking warn.
- **Edges**: SVG lines. Active edges are dashed and animated, with 2 SMIL `animateMotion` particles. The sent-back loop is a dashed warn curve with the label "Sent back: not novel enough".

## 7. Lower panels

### 7.1 Decisions (c7): a timeline

- **Header**: title "Decisions", caption "What the agents decided, and why", and filter tabs: **All** plus one per flow (the code chip).
- **List**: `ol.tl`, max-height 560, scrolls, newest first, capped at 40 items.
- **Each item** is a 3-column grid (58px time | 22px rail | body):
  - **Time**: mono `HH:MM:SS` (24h).
  - **Rail**: a 1px `--line` vertical line, with a 9px ring dot in the flow color. Pivot and escalate items get a filled warn dot.
  - **Body** (bottom hairline):
    - Top line: flow code chip, agent name (muted), and an outcome label pushed right in mono 11px. Outcome colors: Keep = ok; Send back and Needs you = warn; Route and Decide = accent; Merge, Challenge and Deliver = ink-2.
    - "What": 14px/500 ink.
    - "Why": 13px ink-2, max 72ch.
    - Meta (mono 11.5px muted): `conf 0.86`, a 40×2 bar (warn below .70), `N sources`, and the flow's short name.
  - **Escalations** add the buttons **Approve ranking** (solid sm) and **Run another round** (sm). They disappear once resolved.
- **New items** animate in from translateY(−4px) with an accent-soft wash over 1.2s. Items seeded on load don't animate.

### 7.2 Idea Consultant: "How this agent is built" (c5)

This card shows the reusable-setup idea on one concrete agent, and shows exactly where SerpAPI is used.

- **Header**: "Idea Consultant", caption "How this agent is built", and an "Open in builder" sm button that opens the builder with the Idea consultant template.
- **Body**: groups labeled in mono 11px (Model, Tools, Instructions, Monitoring).
- **Rows** are a grid (30px icon tile | name + purpose | step tag) with padding 9px 20px. The step tag is mono 11.5px: "02 Scout", "08 Deliver" and so on, or "inherited".

| Group | Row | Purpose | Step tag |
|---|---|---|---|
| Model | claude-opus-5-5 (cpu) | Hosted on NMC² GPUs | 04 Ideate |
| Tools | **SerpAPI · Google Search** (globe) | Finds competitors and existing products · **{Google+Trends+News} searches today** | **02 Scout** |
| Tools | **SerpAPI · Scholar & Patents** (book-open) | Checks research and prior art · **{Scholar+Patents} searches today** | **02 Scout** |
| Tools | Slack (message-square) | Posts the finished brief | 08 Deliver |
| Instructions | Enterprise base policy (shield) | Same for every agent | inherited |
| Instructions | Consultant brief format (file-text) | Shapes the deliverable | 08 Deliver |
| Monitoring | Tracing, PII redaction, budget (activity) | Same for every agent | inherited |
| Monitoring | Human approval (user-check) | Pauses at the Decide gate | 07 Decide |

- **SerpAPI rows** get a 2px accent bar on the left, an accent icon border and color, and an accent step tag. Their search counts update live as Scout steps finish (+6 per engine used).
- **Hovering any row with a step** highlights that step's column in the river (an accent wash at .06 light / .09 dark). This links "what the agent is built from" to "where it runs".
- **Footer**: "9 shared blocks · 4 inherited from the workspace".

### 7.3 Agent builder (`dialog#builder`, the core interaction)

**Opens from**: the header form, a template chip, "New agent", or the palette action "Create an agent".

**Layout**:

- Width `min(720px, 100% − 32px)`, radius 12, `--pop` shadow.
- **Head**: mono eyebrow ("{Template} template" or "Custom agent" · "Assembled from your workspace"), an editable name input (28px/300), and the goal (14px ink-2). Below it, a 2px progress line that fills to 100% over .9s on open.
- **Body**: four sections (Model, Tools, Instructions, Monitoring). Each is a 116px label column (mono title plus a note: "Workspace default", "Matched to the goal", "Policies are inherited", "On for every agent") next to the block chips.
  - Sections reveal one after another, 160ms + n × 170ms, from opacity .15 and translateY(6px).
- **Chips**: 32px tall, 1px line-2 border, radius 7. Each has the block icon, the name, and a mono tag:
  - "inherited" in muted text for workspace defaults,
  - "suggested" in accent with an accent-tinted border for goal matches.
  - Clicking toggles a chip off (opacity .4, tag gets "· off").
- **Instructions section**: also shows a generated instruction preview in a mono box: "Goal: {goal}.\nWork from approved sources only, cite what you use, and ask a person before any external action."
- **Footer**: mono meta "{N} shared blocks · 0 set up by hand", then Cancel (ghost), "Deploy to staging", and "Deploy agent →" (solid).

**Assembly rules** (regex on the lowercase goal; all matches are merged):

| Match | Tools | Instructions | Monitors | Model |
|---|---|---|---|---|
| support · ticket · customer · refund · reply | Zendesk, Slack | Brand voice, Escalation | Human approval, Quality evals | sonnet |
| contract · clause · legal · vendor · document · pdf · policy | Drive, E-signature | Escalation | Human approval, Quality evals | sonnet |
| metric · data · warehouse · sql · report · dashboard · kpi | Data warehouse, Sheets | – | Quality evals, Latency SLO | sonnet |
| email · inbox · mail | Gmail | Brand voice | Human approval | haiku |
| idea · market · research · competitor · patent · trend · original | Web search, Scholar & patents, Slack | Consultant brief, Escalation | Quality evals | opus |
| incident · on-call · outage · latency · status page · alert | PagerDuty, Slack, Web search | Escalation | Latency SLO | haiku |
| *(no match)* | Drive, Slack | – | – | sonnet |

The first match decides the model. Inherited blocks are always added: Enterprise base policy, Data privacy rules, Tracing, PII redaction, Cost budget.

**Naming**: the template name, or the first 3 non-stopwords of the goal in Title Case.

**Deploying**: close the dialog and increment the agent count everywhere. Toast "Deploying {name}. It will be live in a few seconds.", then "{name} is live" after 2.6s. "Deploy to staging" only toasts. The block catalog (`BLOCKS`) and its usage counts live in JS for the builder's chips.

---

## 8. Simulation (shared by every view)

### 8.1 Stages

| # | key | Label · sub | Base duration (ms) | Gate |
|---|---|---|---|---|
| 0 | brief | Brief · Intake | 1500 | |
| 1 | scout | Scout · **SerpAPI** (column sub shown in accent, weight 500) | 3600 | |
| 2 | map | Map · Landscape | 2600 | |
| 3 | ideate | Ideate · NMC² GPUs | 4600 | |
| 4 | novelty | Novelty · Gate | 2600 | yes |
| 5 | panel | Consult · 3 critics | 5200 | |
| 6 | decide | Decide · Gate | 2600 | yes |
| 7 | deliver | Deliver · Brief | 1600 | |

Lane speed multipliers: `[1, 1.12, .92]`. Flow codes start at F-1041 and increment on every recycle.

### 8.2 Lane state machine

- **Lane state**: `{ i, color, code, s: scenario, states[8], mains[8], subs[8], stage, t, dur, p, pivoted, escalated, waiting, done }`.
- **Tick** every 120ms: `p = (now − t) / dur`. Live counters update during the stage: Scout shows `"{n} via SerpAPI"`, Ideate shows `"{n} ideas"`, Consult switches from "round 1/2" to "round 2/2" halfway. When p ≥ 1, the stage finishes.
- **Running text** (main / sub):
  - brief: reading / client
  - scout: searching / engines joined by " · "
  - map: clustering / `{results} sources`
  - ideate: generating / 16 GPUs
  - novelty: scoring / bar 0.70
  - panel: round 1/2 / 3 critics
  - decide: ranking / nov × feas
  - deliver: writing / consultant brief
- **When each stage finishes** (done text, then the decision logged):

| Stage | Done text (main / sub) | Decision logged (agent · what · outcome · confidence) |
|---|---|---|
| brief | parsed / 4 constraints | – |
| scout | `{results} results` / "SerpAPI · {engines}" | Scout · "Searched {engines joined ' and '} via SerpAPI" · Route · .88 |
| map | `{clusters} clusters` / 2 merged | Cartographer · "Merged 2 overlapping clusters" · Merge · .81 |
| ideate | `{ideas} ideas` / round 1 or 2 | – (idea rows set to Drafted) |
| novelty, `pivot` scenario, first pass | Novelty and Ideate reset to idle | Novelty Judge · "Sent the ideas back for another round" · Send back · .77. Why: "Only 2 of {ideas} ideas cleared the 0.70 novelty bar. Hardware is crowded, so the Ideator will explore service models instead." Shows the loop for 4.2s, then reruns Ideate |
| novelty, otherwise | `{kept} kept` / `{n} pruned` | Novelty Judge · "Kept {kept} of {ideas} ideas" · Keep · .86. Why: "The other {n} matched existing products with more than 0.70 similarity in SerpAPI search results." Idea rows set to Passed novelty |
| panel | 2 rounds / 1 challenged | Consultant panel · "Challenged one idea and kept the rest" · Challenge · .79. Idea rows set to In debate |
| decide, `escalate` scenario, first pass | **waiting**, "your call" / conf 0.64 | Lead Consultant · "Asked you to break a tie" · Needs you · .64, with buttons. Sidebar shows "1 waiting" and the lede link appears. Auto-approves as "Engagement lead" after 15s |
| decide, otherwise | top 3 chosen / best 0.xx | Lead Consultant · "Chose the top 3 ideas" · Decide · .90. Idea rows set to Shortlisted |
| deliver | brief ready / 12 pages | Lead Consultant · "Delivered the brief to {client}" · Deliver · .92. Toast "{code} delivered to {client}". After 6.5s, recycle the lane with the next scenario and a new code |

- **Resolving an escalation**:
  - **Approve**: logs You · "Approved the ranking", then Deliver starts.
  - **Run another round**: logs You · "Asked the panel for another round" · Challenge, then reruns Consult. The flow doesn't escalate again.

### 8.3 Initial frame

- Lanes start at stages `[4, 5, 1]`, 45% of the way into the current stage.
- Earlier stages are completed silently: their decisions are logged with past timestamps and counters don't move.
- Result: F-1041 is about to be sent back, F-1042 is about to escalate, and F-1043 is mid-Scout.

### 8.4 Scenario data (use verbatim)

Each scenario has: `title, short, client, engines[], results, clusters, ideas, kept, pivot?, escalate?, why{scout,map,panel,decide}, list[[idea, novelty, feasibility, sources]×3]`.

1. **Cold-chain for rural clinics** · short `cold-chain` · client "Medical logistics startup" · engines Google, Patents · 42 results · 6 clusters · 24 ideas · 7 kept · **pivot**
   - scout: "The brief describes a physical device, so prior art in Google Patents matters more than news."
   - map: "“Solar fridges” and “off-grid cold boxes” share 81% of their sources, so they are one market."
   - panel: "Feasibility objected to drone delivery: aviation waivers take 9+ months. Risk agreed, 2–1."
   - decide: "Ranked by novelty × feasibility. The cold-box co-op leads at 0.84."
   - ideas: Clinic-owned cold-box rental co-op (.84/.71/14) · Vaccine temperature passports over SMS (.79/.82/9) · Pharmacy-to-clinic relay with moto couriers (.72/.77/17)
2. **AI tutoring for trade schools** · `trade tutoring` · "EdTech accelerator" · Scholar, Google · 57 · 5 · 20 · 6 · **escalate**
   - scout: "Learning-outcome claims need evidence, so Scholar gets 60% of the search budget."
   - map: "Most existing tools target K-12 coding. Hands-on trades are a sparse cluster."
   - panel: "Market liked the AR welding tutor. Feasibility flagged headset cost at $1,100 per student."
   - decide: "Ideas #2 and #3 score within 0.02 and the panel split 2–1. Confidence 0.64 is below the 0.70 auto-approve bar."
   - ideas: Shop-floor AR tutor for welding (.81/.58/11) · Apprentice matching from real job tickets (.77/.74/13) · Licensing exam drills over voice (.69/.86/8)
3. **Water reuse for GPU data centers** · `water reuse` · "NMC² sustainability team" · Google, News, Patents · 64 · 7 · 28 · 9
   - scout: "Regulation changes fast here, so News is included alongside Patents."
   - map: "Closed-loop cooling is saturated with 23 vendors. Partnerships with farms are not."
   - panel: "Risk flagged water-rights law in South Carolina. Market called the farm swap a strong story."
   - decide: "The farm water swap leads on novelty at 0.88 despite a lower feasibility score."
   - ideas: Cooling-tower water swap with local farms (.88/.62/19) · Rain-to-rack storage for summer peaks (.74/.70/12) · Greywater credits for university campuses (.71/.55/10)
4. **Micro-grids for farm co-ops** · `farm micro-grids` · "Rural energy nonprofit" · Google, Trends · 38 · 5 · 22 · 6
   - scout: "Trends shows search interest in co-op solar up 40% this year, so it joins the mix."
   - map: "Utility-run programs dominate. Co-op-owned storage is an open gap."
   - panel: "Feasibility liked shared batteries. Risk worried about interconnect delays."
   - decide: "Shared battery leasing ranks first at 0.81."
   - ideas: Shared battery leasing between neighboring farms (.81/.69/12) · Irrigation pumps as a demand-response fleet (.76/.73/9) · Grain-dryer heat recovery bundles (.70/.80/7)
5. **Wildfire smoke alerts for schools** · `smoke alerts` · "State education office" · News, Scholar · 49 · 6 · 18 · 5
   - scout: "Air-quality research lives in Scholar, and live incidents in News."
   - map: "Consumer AQI apps are crowded. School-specific playbooks are rare."
   - panel: "Market and Risk agreed that liability makes automated recess calls attractive."
   - decide: "Recess-decision bot leads at 0.83."
   - ideas: Automatic recess calls from AQI sensors (.83/.78/10) · HVAC filter swap scheduling from forecasts (.75/.81/8) · Parent text alerts tied to bus routes (.68/.88/6)

Lanes 0–2 start with scenarios 1–3 (codes F-1041..F-1043). Recycling continues from scenario 4 and wraps around.

---

## 9. Copy rules

- Write plain, active sentences. Every control says what it does ("Start flow", "Approve ranking", "Run another round"), and every toast confirms what happened.
- Flows are named by code (`F-1041`) plus a short name. Agents are named by role.
- Numbers use tabular figures. Scores always have 2 decimals.

## 10. Accessibility

- Icon-only buttons have `aria-label`, and the brief input has a label.
- Panels use `aria-labelledby`. The palette uses listbox/option roles with `aria-selected`.
- Toasts are in an `aria-live="polite"` region.
- `:focus-visible` shows a 2px accent outline.
- Every status has text, never color alone.
- Reduced motion is respected everywhere, including the river.

## 11. Build order

1. Skeleton: `<title>NMC³ Console</title>`, fonts, the Lucide script, one `<style>` and one IIFE `<script>`.
2. Tokens and theme blocks (§3), base styles, grain and the brand mark (§4).
3. Shell: sidebar, top bar, header, KPI strip (§5).
4. Panels: Live runs (river plus Steps board), Decisions, the Idea Consultant setup card (§6–7), and the builder dialog (§7.3).
5. Simulation (§8) driving the 2D board, decisions, shortlist, agents, compute and sources. Seed the initial frame and start the intervals: tick 120ms, compute 1.4s, GPU hours 9s.
6. Idea river (§6): canvas, label layer, `layout()`, `draw(t)` with the render loop, and the River/Steps tabs, wrapped in try/catch with a fallback to Steps.
7. Interactions: theme toggle, brief form, drawer, command palette, toasts. Call `lucide.createIcons()` last.
8. Check light and dark at 1440, 1024, 768 and 390px.

## 12. Porting to the Next.js app (`frontend/`)

- Move the tokens into `globals.css`.
- Use `next/font/google` for IBM Plex Sans and Mono, and `lucide-react` for icons.
- Keep the river as a `<canvas>` component driven by `requestAnimationFrame`, with the same geometry and drawing order (§6).
- Components: `Sidebar`, `TopBar`, `PageHeader` (with `BriefForm`), `KpiStrip`, `IdeaRiver` / `StepsBoard`, `DecisionTimeline`, `AgentSetupCard`, `AgentBuilder`, `CommandPalette`, `Toaster`, `ThemeToggle`.
- Put the simulation in a `useFlowSimulation()` hook. Later, replace it with FastAPI data (`/api/...`, already proxied by `next.config.ts`), for example via SSE for live decisions.
- The current `frontend/` still has Ant Design installed. This design doesn't use it.
