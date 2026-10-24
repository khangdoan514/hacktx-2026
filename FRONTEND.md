# FRONTEND.md — NMC³ Console design spec

This document describes the NMC³ Console frontend design completely enough that another engineer or AI agent can rebuild it from scratch and get the same result.

- **Reference implementation:** `docs/design/relay-console.html`. It is one self-contained HTML file with inline CSS and JS. When this document and that file disagree, the file wins.
- **Status:** design prototype. All data is simulated in the browser and nothing calls the backend yet.
- **Later target:** port into `frontend/` (Next.js 15, App Router). See [§17](#17-porting-to-the-nextjs-app).

---

## 1. Product context

**NMC³** is an AI consultant agent platform. A user submits a *brief* (a problem to explore). A pipeline of agents then:

1. researches what already exists using **SerpAPI** (Google Search, Scholar, Patents, Trends, News),
2. generates candidate ideas on **NMC² compute**,
3. filters them for originality,
4. has critic agents debate them,
5. returns a ranked shortlist as a consultant brief.

Background on the name:

- **NMC²** (https://nmc2.com) is an HPC / GPU cloud and compute company with an HPC site in Spartanburg, SC.
- **NMC³** is the consultant layer that runs on NMC². The extra "C" stands for **Consultant**.
- Always write the name with superscripts: **NMC³** and **NMC²**. In HTML that is `NMC<sup>3</sup>`, or the Unicode characters ³ and ².

**What the screen must communicate:**

- Several flows run at once.
- Agents make visible decisions: route, merge, keep/prune, send back, escalate to a human.
- The platform runs on real infrastructure (GPUs, search quota).

**Hard design constraints from the product owner:**

| Do | Don't |
|---|---|
| Left **sidebar** navigation | Top-nav-only layout |
| Multiple concurrent flows, visible agent decisions | A single linear pipeline |
| Glassmorphism, soft ambient background | Gradients/glow on every element |
| **Light and dark theme** plus a toggle button | Single theme |
| One accent color, monochrome UI | Rainbow of category colors |
| Metrics about ideas, novelty, sources, decisions, GPUs | **Anything that looks like financial services**: revenue, $ KPIs, refunds, invoices, budgets |
| Compact stat tiles | A big "hero" metric card (e.g. a large "Today's throughput" panel) |
| Large light-weight headings (weight 400) | Uppercase titles, bold-everything, dense tiny labels |
| Effects and animation, tasteful | Static page |

**Visual reference:** the "AI Fashion Ecommerce Dashboard" shot by taqwah on Muzli/Dribbble. Copy its qualities, not its content:

- light grey canvas, white rounded cards with no borders,
- huge regular-weight headings,
- one lavender accent,
- black pill for the active tab,
- hatched or tick-mark chart styling,
- lots of whitespace.

---

## 2. Tech and dependencies

The reference file is plain HTML, CSS and JS with no framework.

| Dependency | How it's loaded | Used for |
|---|---|---|
| Inter Tight 300–700 | Google Fonts `<link>` | All UI text |
| Geist Mono 400, 500 | Google Fonts `<link>` | IDs, scores, numbers in tables |
| Lucide icons **0.469.0** | `<script src="https://cdn.jsdelivr.net/npm/lucide@0.469.0/dist/umd/lucide.min.js">` | Every icon |

Font link:

```html
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter+Tight:wght@300..700&family=Geist+Mono:wght@400;500&display=swap">
```

Icons are written as `<i data-lucide="name"></i>`. Lucide replaces them with `<svg class="lucide lucide-name">`, keeping any extra class on the `<i>`. After inserting dynamic HTML, call `lucide.createIcons()` again. The reference wraps this in a helper `icons()` and builds icon markup with `ic(name)`.

No other libraries are used. Charts, the flow board and the GPU grid are hand-built HTML/SVG.

---

## 3. Design tokens

Define every token on bare `:root` (light). Redefine only the tokens in two dark blocks:

- `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { … } }`
- `:root[data-theme="dark"] { … }`

Both dark blocks also set `color-scheme: dark`. This gives three states: system default, forced light, forced dark.

### 3.1 Color

| Token | Light | Dark | Role |
|---|---|---|---|
| `--bg` | `#eceef4` | `#0a0a11` | Page background (always set on `html` and `body`) |
| `--orb-1` | `#dccbff` | `#3f2596` | Ambient blob 1 (lavender) |
| `--orb-2` | `#c9d7ff` | `#17307e` | Ambient blob 2 (periwinkle) |
| `--orb-3` | `#d4f1ec` | `#0d4a46` | Ambient blob 3 (mint) |
| `--glass` | `rgba(255,255,255,.58)` | `rgba(26,26,38,.55)` | Card glass fill |
| `--glass-strong` | `rgba(255,255,255,.82)` | `rgba(34,34,48,.8)` | Raised glass (selected seg button, dialog) |
| `--glass-border` | `rgba(255,255,255,.8)` | `rgba(255,255,255,.08)` | Glass edge |
| `--glass-hi` | `inset 0 1px 0 rgba(255,255,255,.95)` | `inset 0 1px 0 rgba(255,255,255,.07)` | Top highlight (box-shadow) |
| `--shadow` | `0 1px 1px rgba(20,18,40,.03), 0 18px 40px -20px rgba(70,48,140,.22)` | `0 18px 40px -20px rgba(0,0,0,.7)` | Card shadow |
| `--node-bg` | `#fbfbfe` | `#1d1d29` | Solid fill of flow nodes |
| `--ink` | `#111118` | `#f3f2f8` | Primary text |
| `--ink-2` | `#4a4a57` | `#b9b8c7` | Body text |
| `--ink-3` | `#8b8b99` | `#7d7c8e` | Muted text |
| `--line` | `rgba(17,17,26,.09)` | `rgba(255,255,255,.09)` | Hairlines, idle borders |
| `--chip` | `rgba(17,17,26,.045)` | `rgba(255,255,255,.06)` | Icon-button and chip fill |
| `--chip-hover` | `rgba(17,17,26,.08)` | `rgba(255,255,255,.11)` | Hover fill, meter track |
| `--solid` / `--solid-fg` | `#111118` / `#fff` | `#f3f2f8` / `#111118` | Primary buttons, active nav pill, toast (inverts in dark) |
| `--accent` | `#7c5cf5` | `#a98bfb` | The single accent (lavender) |
| `--accent-2` | `#6d8bf5` | `#8ea3ff` | Gradient partner (periwinkle) |
| `--accent-soft` | `rgba(124,92,245,.12)` | `rgba(169,139,251,.16)` | Tints, focus ring |
| `--l1` | `#7c5cf5` | `#a98bfb` | Flow lane 1 identity |
| `--l2` | `#3f7cf0` | `#7fa2ff` | Flow lane 2 identity |
| `--l3` | `#0f9d8f` | `#3fd6c6` | Flow lane 3 identity |
| `--ok` | `#1f9d6b` | `#3ddc97` | Success state |
| `--warn` | `#d4831a` | `#f5b14c` | Needs attention, sent back, escalation |
| `--err` | `#e5484d` | `#ff6b70` | Error (reserved) |

Color rules:

- Most pixels are neutral.
- The accent appears in icons, meters, score bars and focus states.
- Lane colors (`--l1..3`) appear **only** to identify which flow something belongs to: lane dot, node border, edge, particles, decision icon tile, idea-row dot.
- Status colors appear only on dots and small labels.

### 3.2 Type

| Token | Value |
|---|---|
| `--font` | `"Inter Tight", "Helvetica Neue", Helvetica, Arial, sans-serif` |
| `--mono` | `"Geist Mono", ui-monospace, "SF Mono", Menlo, monospace` |

Body text is 14px with line-height 1.45, letter-spacing `-.005em`, and `-webkit-font-smoothing: antialiased`.

| Role | Size / weight / tracking |
|---|---|
| Page title `h1` | `clamp(30px, 3.4vw, 40px)` / 400 / `-.035em`, line-height 1.05 |
| Card title `h2` | 22px / 400 / `-.03em` |
| Stat value | 28px / 400 / `-.04em`, tabular numbers; unit in an `<em>` at 15px muted |
| Card meta / subtitles | 13px, `--ink-3` |
| Node main text | 12.5px / 500; node sub text 11px muted |
| Table body | 13.5px; scores in mono 12px |
| Small labels | 11–12.5px, never uppercase, never letter-spaced |

Use `font-variant-numeric: tabular-nums` on every changing number.

### 3.3 Shape, motion, misc

| Token | Value |
|---|---|
| `--r` | `20px` (card radius) |
| `--ease` | `cubic-bezier(0.16, 1, 0.3, 1)` (every UI transition) |

Other radii:

- inputs and stat tiles: 16px
- top bar: 18px
- icon buttons: 10px
- small buttons: 8px
- flow nodes: 13px (gates are hexagons; see §7.3)

Register `@property --ang { syntax: "<angle>"; initial-value: 0deg; inherits: false; }`. It drives the rotating conic border on running nodes.

---

## 4. Global primitives

1. **Ambient field.** A `div.ambient` is fixed at `inset:0` with `z-index:-1` and `pointer-events:none`. It contains three `<i>` blobs:
   - Each blob is round, uses `filter: blur(90px)` and `opacity:.85`, and runs `drift` 30s ease-in-out infinite alternate. The keyframe goes to `translate3d(4vw,3vh,0) scale(1.08)`.
   - Blob 1: 50vw square at left −10vw, top −20vw, `--orb-1`.
   - Blob 2: 44vw at right −12vw, top 25vh, `--orb-2`, delay −10s.
   - Blob 3: 36vw at left 30vw, bottom −20vw, `--orb-3`, delay −18s.
2. **`.glass`**:

   ```css
   .glass {
     background: var(--glass);
     border: 1px solid var(--glass-border);
     border-radius: var(--r);
     backdrop-filter: blur(28px) saturate(170%); /* also the -webkit- version */
     box-shadow: var(--glass-hi), var(--shadow);
   }
   ```

   Every card, the sidebar, the top bar and the brief bar use it.
3. **`.ibtn`**: 36×36 icon button, radius 10, no border, `--chip` fill, `--ink-2` icon.
   - Hover: `--chip-hover`, `--ink`, translateY(−1px).
   - Active: scale(.95).
4. **`.btn`**: 36px tall, padding 0 14px, radius 10, `--chip` fill, weight 500.
   - Hover: translateY(−1px). Active: scale(.97).
   - `.solid`: `--solid` background with `--solid-fg` text. Hover adds `0 10px 24px -10px` of accent at 70%.
   - `.sm`: 30px tall, 12.5px text, radius 8.
   - `.text`: transparent, muted.
5. **Status dot `.dot`**: 7px circle.
   - Variants: `.ok`, `.run` (accent), `.warn`, `.idle` (opacity .5).
   - `.live` adds an `::after` ping ring: `inset:-4px`, currentColor, keyframes scale .4→1.8 and opacity .5→0, 2s, infinite.
   - A status is always a dot plus plain text, never a filled badge.
6. **`kbd`**: mono 11px, chip background, radius 5.
7. **Focus**: `:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }`.
8. **Lucide icons**: 17px default, `stroke-width: 1.6`.
9. **Entrance**: `.rise` runs keyframes from `translateY(14px)` and opacity .3 over .8s with `--ease`, fill `both`. Stagger it with an inline `--d` delay (0–430ms). Content never starts at opacity 0.
10. **Reduced motion**: under `prefers-reduced-motion: reduce`, set every animation to .001ms and 1 iteration, and every transition to .001ms. The JS also skips the typing placeholder, the particles and the theme reveal.

---

## 5. Page structure

```
body
├─ .ambient (3 blobs)
├─ .shell  (grid: 252px sidebar | 1fr main, gap 16, padding 14 + safe-area)
│  ├─ aside.side.glass          ← sidebar (§6.1)
│  ├─ .backdrop                 ← mobile drawer scrim
│  └─ .main (flex column, gap 16)
│     ├─ header.top.glass       ← top bar (§6.2)
│     ├─ section.head           ← page title + actions (§6.3)
│     ├─ section.brief.glass    ← "new brief" input (§6.4)
│     ├─ section.stats          ← 5 stat tiles (§6.5)
│     └─ main.grid (12 cols, gap 16)
│        ├─ Live flows      span 12   (§7)
│        ├─ Decisions       span 5    (§8)
│        ├─ Idea board      span 7    (§9)
│        ├─ Agents          span 4    (§10)
│        ├─ NMC² compute    span 4    (§11)
│        └─ SerpAPI sources span 4    (§12)
├─ dialog#palette  (⌘K command palette, §13.4)
└─ .toasts         (bottom-center toasts, §13.5)
```

Cards use `.card.glass` with padding 22px 24px and are flex columns. A card header `.card-h` is a flex row with:

- left: `h2` plus a `.meta` line,
- right: `.right`, with controls pushed right by `margin-left:auto`.

---

## 6. Shell components

### 6.1 Sidebar (`aside.side.glass`)

- Sticky at `top: 14px + safe-area`, `height: calc(100vh - 28px)`, padding 16px 12px, flex column, `overflow-y:auto`.
- **Brand row**:
  - A 32px black rounded square (`--solid`, radius 10) with white "N³" in 12px bold.
  - Beside it, "NMC<sup>3</sup>" in 17px/600 and the caption "Consultant on NMC² compute" in 11.5px muted.
- **Workspace switcher**:
  - A 40px chip button: a 22px gradient tile reading "HX" (accent-2→accent, 140°), the text "HackTX workspace", and a `chevrons-up-down` icon.
- **Nav items**: 38px tall, radius 10, `--ink-2` text, 17px icon, gap 11.
  - The current item (`aria-current="page"`) is a **black pill**: `--solid` background, `--solid-fg` text.
  - Items, in order:
    - Overview `layout-dashboard` (current)
    - Flows `workflow`, with a tag showing a live dot and "3"
    - Ideas `lightbulb`, tag "9"
    - Decisions `git-fork`, with a warn-tinted tag "1" that only shows while a flow is waiting on the user
    - Agents `bot`
  - Then the group label "Infrastructure" (12px muted), followed by:
    - Sources · SerpAPI `search`
    - Compute · NMC² `cpu`
    - Settings `settings`
- **Footer**: pinned with `margin-top:auto` above a top hairline. It holds two meters (5px track, accent-2→accent gradient fill):
  - "NMC² GPU load" with a %
  - "SerpAPI quota" showing "1,846 / 5,000"
  - Both update live.

### 6.2 Top bar (`header.top.glass`)

- Sticky, 60px tall, radius 18, padding 0 12px 0 18px.
- Left: a hamburger `.ibtn` (mobile only), then breadcrumbs reading "HackTX workspace / **Overview**".
- Right:
  - A search button: 280px chip, `search` icon, "Search flows, ideas, agents" and a `⌘K` kbd. It opens the palette.
  - A bell `.ibtn`.
  - The **theme toggle** `.ibtn` (§13.1).
  - The avatar: a 36px tile with radius 10, an accent gradient and the initials "KD".

### 6.3 Page head

- `h1`: "Idea flows".
- Sub line: "**3** flows running · **4** briefs queued · agents made **1,092** decisions today". The three numbers are live.
- Actions on the right: a "Thresholds" `.btn` with `sliders-horizontal` icon, and a "New brief" `.btn.solid` with `plus` icon. New brief focuses the brief input.

### 6.4 Brief bar (`section.brief.glass`)

- Radius 16, padding 8px 8px 8px 16px.
- Contents, left to right: a `sparkles` icon in accent, then a text input (15.5px, no border), two option chips, and "Start flow →" `.btn.solid`.
  - Option chips: "Google, Scholar, Patents" and "Novelty ≥ 0.70".
- The placeholder types itself out and erases:
  - 34ms per character while typing, 16ms per 2 characters while erasing, with a 2.2s pause when complete.
  - It cycles through: "Affordable heat pumps for older apartment buildings", "Reduce food waste in university dining halls", "Faster permits for backyard housing units", "Safer night commutes for gig workers".
  - While focused or filled, it shows "What problem should NMC³ explore?".
- On focus, add `0 0 0 4px var(--accent-soft)` to the shadow.
- Enter or Start: if empty, focus the input. Otherwise clear it, add 1 to the queued count, and toast "Brief queued. It starts when a flow slot frees up."

### 6.5 Stat tiles (`.stats`)

- A 5-column grid with gap 12.
- Each tile is `.stat.glass` with radius 16 and padding 16px 18px. It shows a small label with an accent icon, a big value, and a caption.

| Label (icon) | Value | Caption |
|---|---|---|
| Flows running (`workflow`) | `3` + `<em>of 6 slots</em>` (live) | 4 briefs in queue |
| Ideas generated (`lightbulb`) | `214`, live | +38 in the last hour (green) |
| Passed novelty (`git-fork`) | `14` + `<em>%</em>` | 31 of 214 cleared 0.70 |
| Sources read (`search`) | `3,912`, live | via SerpAPI today |
| Brief to shortlist (`timer`) | `6<em>m</em> 40<em>s</em>` | 1m 12s faster than last week (green) |

---

## 7. Live flows board (the centerpiece)

The card header has:

- title "Live flows",
- meta "Each row is one client brief moving through the NMC³ pipeline. Click a row to follow its decisions.",
- a legend on the right: "Agent step" (rounded outline), "Decision gate" (small hexagon), "Sent back" (dashed warn line).

Below that is `.flows-wrap`, with `overflow-x:auto` and negative margins to the card edges. Inside it is `.flows`, a `position:relative` canvas with `min-width:980px`. The canvas holds absolutely positioned column headers, lane rows and nodes, plus an SVG overlay (`svg.edges`, full size, `pointer-events:none`) for edges and particles.

### 7.1 Pipeline stages (columns)

| # | key | Column label | Sub | Icon | Agent(s) | Base duration | Gate? |
|---|---|---|---|---|---|---|---|
| 0 | brief | Brief | Intake | `file-text` | Intake | 1500ms | |
| 1 | scout | Scout | SerpAPI | `search` | Scout | 3600 | |
| 2 | map | Map | Landscape | `map` | Cartographer | 2600 | |
| 3 | ideate | Ideate | NMC² GPUs | `sparkles` | Ideator | 4600 | |
| 4 | novelty | Novelty | Decision | `git-fork` | Novelty Judge | 2600 | **yes** |
| 5 | panel | Consult | 3 critics | `messages-square` | Market / Feasibility / Risk critics | 5200 | |
| 6 | decide | Decide | Decision | `scale` | Lead Consultant | 2600 | **yes** |
| 7 | deliver | Deliver | Brief | `send` | Lead Consultant (and Intake) | 1600 | |

A lane runs each stage for `base × SPEED[lane]`, where `SPEED = [1, 1.12, .92]`.

### 7.2 Geometry (recompute on resize with `ResizeObserver` on `.flows-wrap`)

```
W       = max(wrap.clientWidth − 48, 980)
LANE_H  = 92, HEAD_H = 44, LABEL_W = 184
H       = HEAD_H + lanes × LANE_H + 8
colW    = (W − LABEL_W) / 8
nodeW   = min(colW − 12, 128); node height 50
column j header: left = LABEL_W + j·colW, width colW, height 40
lane i: top = HEAD_H + i·LANE_H, height LANE_H − 6, center cy = top + (LANE_H−6)/2
node (i,j): x = LABEL_W + j·colW + (colW − nodeW)/2, y = cy − 25
edge j→j+1: straight line M(x_j+nodeW, cy) L(x_{j+1}, cy), id "e{i}-{j}"
loop (Novelty→Ideate): M(cx4, y) C(cx4, y−22) (cx3, y−22) (cx3, y)  — arcs above the lane
loop label: centered between cx3 and cx4 at y−18, text "Sent back: not novel enough"
```

- Column headers are centered: label in 13px/500 `--ink` with a 14px icon (the icon is accent-colored for gate columns), and the sub text in 12px muted below it.
- A **lane row** (`.lane`) spans the full width, has radius 16, and sets `--lc` to its lane color.
  - Hover: `--chip` background.
  - Selected (`.sel`): background `color-mix(lc 7%)` and an inset 1px ring at `color-mix(lc 25%)`.
  - When another lane is selected, this lane gets `.dim` (opacity .38) and its nodes go to opacity .35.
  - Clicking a lane toggles the selection (§8.3).
- The **lane label** (left, 176px wide) has three lines:
  - a lane-color dot with the title (13.5px/500, wraps to 2 lines),
  - the client name (12px muted, ellipsis),
  - a 3px progress bar in the lane color (done steps ÷ 8) followed by text: "Step N of 8", "Needs you" or "Delivered".

### 7.3 Nodes

- Each node is a `.node` (`pointer-events:none`) that acts as the border: a 1px padding shell whose `background` is the border color. Inside it is `.in`, filled with solid `--node-bg` at radius 12.
- Content:
  - `.m` (main) row: a 12px state glyph `.g` followed by the main text.
  - `.s` (sub) row: 11px muted text.
- **Gate nodes** (`.gate`) apply this to both the shell and `.in`, with radius 0 and inner padding 0 15px:

  ```css
  clip-path: polygon(15px 0, calc(100% - 15px) 0, 100% 50%, calc(100% - 15px) 100%, 15px 100%, 0 50%);
  ```

  That turns them into hexagonal "decision gates".
- States (`data-state`):

| State | Shell (border) | Glyph `.g` | Other |
|---|---|---|---|
| idle | `--line` | 1.5px dashed ring | opacity .45, empty text |
| running | `conic-gradient(from var(--ang), lc 15% 0 50%, lc 80%, lc 15% 100%)`, animated `--ang` 0→360deg in 1.8s linear infinite | spinning ring (2px, top colored lane, .8s) | translateY(−2px). Non-gates also get `0 0 0 4px lc 14%` and `0 12px 24px -14px lc` |
| done | `color-mix(lc 45%, line)` | solid lane-colored circle with white `check` | |
| waiting | `--warn`, pulsing between warn and warn 35% every 1.6s | solid warn circle | used when Decide escalates |

### 7.4 Edges and particles

- `path.edge` uses stroke `--line` at 1.6 width. Set `--lc` on each path.
  - `.done` (when the target is done): stroke lane at 55%.
  - `.active` (source done, target running or waiting): stroke lane at 75% with `stroke-dasharray: 3 5`, animated dashoffset 0→−8 every .6s.
- **Particles**: while an edge is `.active`, add a `<g>` holding two `<circle r=3 fill=lane>`, each with SMIL `<animateMotion dur="1.1s" repeatCount="indefinite" begin="0s|.55s"><mpath href="#e{i}-{j}"/></animateMotion>`. Remove the group when the edge stops being active. Skip particles under reduced motion.
- **Loop edge** `.edge.loop`:
  - Normal: warn color, dash 4 5, opacity 0.
  - `.on`: opacity 1 with the dash animation, plus the loop label shown (fade and scale in) for 4.2s.
  - After that it becomes `.was` (opacity .35).

### 7.5 Simulation (state machine)

Each lane holds the following state:

```
{ i, color, s: scenario, states[8], mains[8], subs[8], stage, t (start time), dur,
  pivoted, escalated, waiting, done, live }
```

A `tick()` runs every 120ms. For each lane that isn't `done` or `waiting`, it computes progress `p = (now − t) / dur`:

- During Scout, the main text counts up: `${n} found` (n = p × results).
- During Ideate, the main text counts up: `${n} ideas`.
- During Consult, the main text switches from "Round 1/2" to "Round 2/2" at p > .5.
- When p ≥ 1, call `finishStage`.

`startStage(j)` sets the stage to running and sets its running text:

| Stage | Main (running) | Sub (running) |
|---|---|---|
| brief | Reading | client name |
| scout | Searching | engines joined with ", " |
| map | Clustering | `{results} sources` |
| ideate | Generating | 16 GPUs |
| novelty | Scoring | Bar 0.70 |
| panel | Round 1/2 | 3 critics |
| decide | Ranking | Nov × feas |
| deliver | Writing | Consultant brief |

`finishStage(j)` marks the stage done, sets its final text, logs a decision and advances:

| Stage | Main / Sub when done | Decision logged (agent · what · outcome) | Side effects |
|---|---|---|---|
| brief | Parsed / 4 constraints | – | |
| scout | `{results} results` / engines | Scout · "Routed searches to {engines joined ' and '}" · **route**, conf .88, cites results | +6 searches per engine used; sources read += results |
| map | `{clusters} clusters` / 2 merged | Cartographer · "Merged 2 overlapping clusters" · **merge**, .81 | |
| ideate | `{ideas} ideas` / Round 1 or Round 2 | – | ideas generated += ideas; idea rows set to "Drafted" (idle dot) |
| novelty, **pivot** (scenario `pivot:true`, first time only) | Novelty and Ideate reset to idle | Novelty Judge · "Sent ideas back for another round" · **pivot**, .77. Why: "Only 2 of {ideas} ideas cleared the 0.70 novelty bar. Hardware is crowded, so the Ideator will explore service models instead." | Show the loop edge and label, then restart Ideate |
| novelty, normal | `{kept} kept` / `{ideas−kept} pruned` | Novelty Judge · "Kept {kept} of {ideas} ideas" · **keep**, .86. Why: "The other {n} matched existing products with more than 0.70 similarity in SerpAPI results." | Idea rows set to "Passed novelty" (ok dot) |
| panel | 2 rounds / 1 idea challenged | Consultant panel · "Challenged one idea, kept the rest" · **challenge**, .79 | Idea rows set to "In debate" (run dot) |
| decide, **escalate** (scenario `escalate:true`, first time) | state **waiting**, "Needs you" / "Confidence 0.64" | Lead Consultant · "Asked you to break a tie" · **escalate**, .64, with buttons **Approve ranking** and **Run another round** | Sidebar Decisions tag shows "1". Auto-resolves as "Engagement lead" after 14s |
| decide, normal | Top 3 chosen / "Best 0.{top novelty}" | Lead Consultant · "Chose the top 3 ideas" · **decide**, .90 | Idea rows set to "Shortlisted" (ok dot) |
| deliver | Brief ready / 12 pages · 31 sources | Lead Consultant · "Delivered the brief to {client}" · **deliver**, .92. Why: "Top pick: {idea 1}." | Toast "{title}: consultant brief delivered". After 6s, **recycle** the lane with the next scenario |

How an escalation resolves:

- **Approve** (or the 14s timeout): Decide becomes done ("Approved by you" or "Approved by lead"). Log "Approved the ranking" with agent You or Engagement lead and outcome decide. Idea rows become Shortlisted, then Deliver starts.
- **Run another round**: log You · "Asked the panel for another round" · challenge. Reset Decide to idle and restart Consult. That flow doesn't escalate again.

**Recycle** fills the lane with `SCENARIOS[next % 5]`, resets all fields and starts at stage 0.

**Initial frame.** On load, each flow should already be partway through:

- Lane starting stages are `[4, 5, 1]`.
- Fast-forward the earlier stages silently: log their decisions with past timestamps, but don't increment counters.
- Then start the current stage with `t` pushed back by 45% of its duration.
- Result: Lane 1 is mid-Novelty (and about to pivot), Lane 2 is mid-Consult (and about to escalate), and Lane 3 is mid-Scout.

### 7.6 Scenario data (use verbatim)

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

Lanes 0–2 start with scenarios 1–3. Recycling continues from scenario 4 and wraps around.

---

## 8. Decisions card (span 5)

### 8.1 Header

- Title "Decisions", meta "What the agents decided and why".
- On the right, a segmented filter `.seg`: **All**, then one button per lane showing a lane-color swatch and the lane's `short` name.
- Segmented control styling: chip track with 3px padding. Buttons are 30px tall. The pressed button uses `--glass-strong` with a small shadow.

### 8.2 Decision item (`li.dec`)

- List: `ul.stream`, max-height 470px, scrolls, newest on top, capped at 40 items.
- Each item is a grid with a 30px icon column and a content column, padding 12px 10px, radius 14, hover `--chip`. `--lc` is set to its lane color.
- **Icon tile**: 30px, radius 9, background `lc 14%`, icon in the lane color. The icon depends on the outcome:
  - route → `route`, merge → `git-merge`, keep → `filter`, pivot → `undo-2`
  - challenge → `messages-square`, decide → `scale`, escalate → `hand`, deliver → `send`
- **Top line** (12px muted): lane short name (in lane color, weight 500), "·", agent name, and a relative time on the right ("now", "12s ago", "3m ago", refreshed every 5s).
- **What line**: 14px/500 `--ink`, followed by an outcome label (11px/600, radius 6, padding 1px 7px):
  - keep: ok tint
  - pivot / escalate: warn tint
  - route / decide: accent tint
  - everything else: neutral chip
- **Why**: 13px `--ink-2`.
- **Meta**: "Confidence", a 46×4 bar (lane color, or warn below .70), the value to 2 decimals, and "{n} sources cited" when there are sources.
- **Escalation items** add two buttons, "Approve ranking" (solid sm) and "Run another round" (sm). They disappear once the escalation is resolved.
- **New items** get `.new`, which animates from `translateY(-6px)` with an accent tint over 1s. Items pre-seeded on load don't animate. Never start an item at opacity 0.
- Each live decision adds 1 to the "decisions today" counter.

### 8.3 Lane selection

Selecting a lane, either by clicking its row or its filter button, does three things:

- highlights that lane and dims the others,
- hides decision items from other lanes,
- updates the pressed state of the filter.

Clicking the selected lane again, or "All", clears the selection.

---

## 9. Idea board (span 7)

- Header: title "Idea board", meta "Best candidates across all flows, ranked by novelty", and an "Export" `.btn.sm` with `download` icon.
- The table bleeds to the card edges. Columns: **Idea · Novelty · Feasibility · Status**.
- Cell padding is 11px 12px, except the first and last columns use 24px on the outer side. Header cells are 12.5px muted at weight 450.
- **Idea cell**: name in 500 weight. Below it, a lane dot with `{short} · {sources} sources` in 12px muted.
- **Score cells**: a bar up to 56px wide (5px tall, accent-2→accent gradient, width = score) followed by the score in mono to 2 decimals.
- **Status**: a dot with text: Drafted (idle), Passed novelty (ok), In debate (run), Shortlisted (ok).
- Rows are deduplicated by idea name across all flows, sorted by novelty descending, and limited to 9. Changed rows flash once with an accent background fading over 1.4s.
- The sidebar "Ideas" tag shows the number of unique ideas.

---

## 10. Agents card (span 4)

- Header: title "Agents", meta "9 agents, shared across flows".
- Rows: 34px icon tile (accent-soft background, accent icon, radius 10), name (13.5px/500), a "doing" line (12px muted, ellipsis), and on the right one 7px dot per flow the agent is currently serving, in that lane's color.

| Agent | Icon | Works stages | Idle text |
|---|---|---|---|
| Intake | file-text | brief, deliver | Waiting for a new brief |
| Scout | search | scout | Idle, SerpAPI quota at 37% |
| Cartographer | map | map | Idle |
| Ideator | sparkles | ideate | Idle, GPUs released |
| Novelty Judge | git-fork | novelty | Idle, threshold 0.70 |
| Market critic | trending-up | panel | Idle |
| Feasibility critic | wrench | panel | Idle |
| Risk critic | shield-alert | panel | Idle |
| Lead Consultant | scale | decide, deliver | Idle |

The "doing" text comes from the first active flow:

- brief: `Parsing “{short}” into constraints`
- scout: `Searching {engines} for {short}`
- map: `Clustering {results} sources for {short}`
- ideate: `Generating ideas for {short}`
- novelty: `Scoring {ideas} ideas for {short}`
- panel: `Debating the shortlist for {short}`
- decide: `Ranking finalists for {short}`, or while waiting: `Waiting for your call on {short}`
- deliver: `Writing the brief for {short}`

While an agent is working, its doing text gets a moving **sheen**: a gradient text-clip from ink-3 to ink to ink-3, background-size 260%, animated over 2.6s linear.

---

## 11. NMC² compute card (span 4)

- Header: title "NMC² compute", meta "Spartanburg HPC site · 64 GPUs", and on the right a status reading "Healthy" with a live ok dot.
- **GPU heat grid**: a 16×4 grid of 64 squares (gap 4, radius 3, aspect 1). Each square is filled with `color-mix(in srgb, var(--accent) calc(var(--u) * 100%), var(--chip-hover))`, transitioning over .9s.
  - Every 1.4s, compute `target = min(.96, .38 + .26 × (#lanes in Ideate) + .10 × (#lanes in Consult))`.
  - Squares are grouped into 4 blocks of 16. The first `round(target × 4 + .4)` blocks are busy: `u = .55 + wobble × .45`. Other blocks are mostly idle: `u = wobble × .18`.
  - `wobble = (sin(now/1400 + i×1.7) + 1) / 2`.
- Caption row: "Each square is one GPU" and "Idle → busy".
- Three key-value tiles (chip background, radius 12):
  - Utilization: the average of u, as a %. This also drives the sidebar meter.
  - GPU hours today: starts at 312, +1 every 9s.
  - Jobs queued: `max(0, 3 − active jobs)`.
- **Job list**: one row per lane in Ideate ("Idea generation · {short}", 16 GPUs) or Consult ("Critic debate · {short}", 8 GPUs), each with a lane-colored live dot. With no jobs, show "No GPU jobs right now · 0 GPUs".

---

## 12. SerpAPI sources card (span 4)

- Header: title "SerpAPI sources", meta "Searches today, by engine".
- One row per engine: icon, name, and a mono count on the right, with a 5px gradient bar below (width = count ÷ max count).

| Engine | Icon | Start count | Scenario key |
|---|---|---|---|
| Google Search | globe | 812 | Google |
| Google Scholar | graduation-cap | 431 | Scholar |
| Google Patents | scroll-text | 297 | Patents |
| Google Trends | chart-line | 188 | Trends |
| Google News | newspaper | 118 | News |

- Footer (above a hairline): "Daily quota" showing `{sum} of 5,000` with a meter. Keep this in sync with the sidebar quota meter.
- Each live Scout completion adds 6 to each engine it used.

---

## 13. Interactions

### 13.1 Theme toggle

- The button contains both a `moon` icon (`.i-moon`) and a `sun` icon (`.i-sun`).
- JS keeps `data-eff="light|dark"` on `<html>`, set to the explicit `data-theme` if present, otherwise the system preference. CSS shows the moon in light and the sun in dark.
- Click: set `data-theme` to the opposite of the current effective theme and save it to `localStorage["nmc3-theme"]` (wrapped in try/catch). Restore it on load.
- **Circular reveal**:
  - If `document.startViewTransition` exists and reduced motion is off, run the change inside a view transition.
  - Animate `::view-transition-new(root)` with `clipPath` from `circle(0 at btnX btnY)` to `circle(maxDistanceToCorner at btnX btnY)` over 650ms using `--ease`.
  - Set `::view-transition-old/new(root) { animation:none; mix-blend-mode:normal }`.
- Shortcut: ⌘/Ctrl + Shift + L.
- Icon hover: rotate −25°.

### 13.2 Escalation buttons

Handled with event delegation on `[data-esc]`. Each button resolves the waiting lane (§7.5) and toasts:

- Approve: "Ranking approved. Writing the brief."
- Run another round: "Panel is running another round".

### 13.3 Mobile drawer

- Below 900px the sidebar becomes a fixed drawer: `left/top/bottom 10px`, `width min(86vw, 290px)`, `--glass-strong`, `translateX(-110%)`. The class `.shell.drawer` brings it in with a .4s ease.
- The hamburger opens it. The scrim or Escape closes it.

### 13.4 Command palette

- Open with ⌘/Ctrl+K (toggles) or the search button. It's a native `<dialog>` using `showModal()`, styled as glass:
  - 20px radius, `--glass-strong`, blur 30px.
  - Backdrop: `rgba(10,10,20,.25)` with a 4px blur.
  - Opens with a pop animation from translateY(8px) scale(.98).
- Layout: a 56px input row (search icon, input, `esc` kbd), then a grouped list.
- Groups:
  - **Actions**: Start a new brief, Adjust novelty threshold (hint 0.70), Switch theme (⌘⇧L).
  - **Flows**: one per lane, hint `step N/8` or `delivered`. Choosing one selects the lane and scrolls to the board.
  - **Agents**: the first 4 agents.
- Up/down arrows move the selection, Enter chooses, and mouse hover selects. The selected row gets `--chip-hover` and an accent icon.
- With no matches, show "No matches. Press Enter to queue "{q}" as a new brief."

### 13.5 Toasts

- Fixed at the bottom center. Each toast is a 44px pill with radius 22: `--solid` background, `--solid-fg` text, an accent `circle-check` icon.
- Animates in from translateY(14px) scale(.96). Removed after 2.6s with a fade-down.

---

## 14. Responsive behavior

| Width | Changes |
|---|---|
| ≤ 1280px | Stats show the first 3 tiles. Decisions and Idea board go full width. Agents and Compute each take half. SerpAPI goes full width. Search field narrows to 220px |
| ≤ 900px | Sidebar becomes the drawer (§13.3) and the hamburger appears. Search collapses to an icon button. Workspace crumb is hidden |
| ≤ 640px | Shell padding drops to 10px. Stats become 2 columns (3rd tile hidden). All cards go full width. Brief option chips are hidden. Card padding becomes 20px 18px. Breadcrumbs are hidden |

- The flow board never shrinks below 980px. It scrolls horizontally inside its card, and the page itself never scrolls sideways.
- Side padding uses `max(Npx, env(safe-area-inset-*))`.
- Include `<meta name="claude:viewport-fit-sides" content="cover">` only when publishing as a Claude artifact.

---

## 15. Accessibility checklist

- Every icon-only button has an `aria-label`.
- The brief input has a visually hidden `<label>`.
- Sections use `aria-labelledby` pointing at their `h2`.
- The palette list has `role="listbox"` and its items have `role="option"` with `aria-selected`.
- The toast container has `aria-live="polite"`.
- Focus is always visible through `:focus-visible`.
- Every animation respects reduced motion (§4.10).
- Status is never conveyed by color alone: each dot has a text label.

---

## 16. Build order (step by step)

1. Create the HTML skeleton: `<title>NMC³ Console</title>`, the font link, the Lucide script, and one `<style>` and one `<script>` block. The script is wrapped in an IIFE.
2. Write the tokens (§3) with all three theme blocks, then the base reset (box-sizing, body font, `html` and `body` background `--bg`).
3. Add the ambient field and `.glass` (§4), then the buttons, dots and `.rise`.
4. Build the shell grid, sidebar, top bar, page head, brief bar and stat tiles (§5–6).
5. Build the Live flows board:
   1. Define the stages, then create the column headers.
   2. For each of the 3 lanes, create the lane row, 8 nodes, 7 edge paths, the loop path and the loop label.
   3. Write `layout()` and attach the ResizeObserver.
   4. Write `render(lane)` to sync node states and text, edge classes, particles and the lane label.
6. Build the Decisions card: `decide(lane, {...})`, relative time refresh, the filter, `selectLane()`.
7. Build the Idea board: `upsertIdeas(lane, status, dot)` and `renderIdeas()`.
8. Build the Agents, Compute and SerpAPI cards and their render functions.
9. Write the simulation: `startStage`, `finishStage`, `resolveEsc`, `recycle`, `tick`. Seed the initial frame (§7.5), then start the intervals: tick 120ms, compute 1.4s, GPU hours 9s, relative times 5s.
10. Add the interactions: theme toggle with view transition, brief composer typing, drawer, command palette, toasts. Call `lucide.createIcons()` last.
11. Check light and dark at 1440, 1024, 768 and 390px. Confirm the page is complete at rest: no element starts invisible and every card has content on the first frame.

---

## 17. Porting to the Next.js app

The repo's `frontend/` folder is Next.js 15 with the App Router, TypeScript and Tailwind v4. It currently also has Ant Design v6, but this design does **not** use Ant Design components. Either remove antd or keep it only for complex form widgets later.

To port the design:

- Move the tokens into `frontend/src/app/globals.css`, using the same three theme blocks.
- Use `next/font/google` for Inter Tight and Geist Mono, and `lucide-react` instead of the UMD script.
- Suggested components:
  - `Sidebar`, `TopBar`, `BriefBar`, `StatTiles`
  - `FlowBoard` (with `FlowLane`, `FlowNode`, `FlowEdges`)
  - `DecisionStream`, `IdeaBoard`, `AgentList`, `ComputeCard`, `SourcesCard`
  - `CommandPalette`, `Toaster`, `ThemeToggle`
- Put the simulation in a client-side hook (`useFlowSimulation`) that returns lanes, decisions and ideas. Later, swap it for real data from FastAPI (`/api/...`, already proxied by `next.config.ts`), for example via SSE or WebSocket for live decisions.
- Keep the geometry math from §7.2 so the flow board looks identical.
