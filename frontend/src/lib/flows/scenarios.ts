// Scenario data from FRONTEND.md §8.4, used verbatim.

export interface Scenario {
  title: string;
  short: string;
  client: string;
  engines: string[];
  results: number;
  clusters: number;
  ideas: number;
  kept: number;
  pivot?: boolean;
  escalate?: boolean;
  why: { scout: string; map: string; panel: string; decide: string };
  /** [idea, novelty, feasibility, sources] */
  list: [string, number, number, number][];
}

export const SCENARIOS: Scenario[] = [
  {
    title: "Cold-chain for rural clinics", short: "cold-chain", client: "Medical logistics startup",
    engines: ["Google", "Patents"], results: 42, clusters: 6, ideas: 24, kept: 7, pivot: true,
    why: {
      scout: "The brief describes a physical device, so prior art in Google Patents matters more than news.",
      map: "“Solar fridges” and “off-grid cold boxes” share 81% of their sources, so they are one market.",
      panel: "Feasibility objected to drone delivery: aviation waivers take 9+ months. Risk agreed, 2–1.",
      decide: "Ranked by novelty × feasibility. The cold-box co-op leads at 0.84.",
    },
    list: [
      ["Clinic-owned cold-box rental co-op", 0.84, 0.71, 14],
      ["Vaccine temperature passports over SMS", 0.79, 0.82, 9],
      ["Pharmacy-to-clinic relay with moto couriers", 0.72, 0.77, 17],
    ],
  },
  {
    title: "AI tutoring for trade schools", short: "trade tutoring", client: "EdTech accelerator",
    engines: ["Scholar", "Google"], results: 57, clusters: 5, ideas: 20, kept: 6, escalate: true,
    why: {
      scout: "Learning-outcome claims need evidence, so Scholar gets 60% of the search budget.",
      map: "Most existing tools target K-12 coding. Hands-on trades are a sparse cluster.",
      panel: "Market liked the AR welding tutor. Feasibility flagged headset cost at $1,100 per student.",
      decide: "Ideas #2 and #3 score within 0.02 and the panel split 2–1. Confidence 0.64 is below the 0.70 auto-approve bar.",
    },
    list: [
      ["Shop-floor AR tutor for welding", 0.81, 0.58, 11],
      ["Apprentice matching from real job tickets", 0.77, 0.74, 13],
      ["Licensing exam drills over voice", 0.69, 0.86, 8],
    ],
  },
  {
    title: "Water reuse for GPU data centers", short: "water reuse", client: "NMC² sustainability team",
    engines: ["Google", "News", "Patents"], results: 64, clusters: 7, ideas: 28, kept: 9,
    why: {
      scout: "Regulation changes fast here, so News is included alongside Patents.",
      map: "Closed-loop cooling is saturated with 23 vendors. Partnerships with farms are not.",
      panel: "Risk flagged water-rights law in South Carolina. Market called the farm swap a strong story.",
      decide: "The farm water swap leads on novelty at 0.88 despite a lower feasibility score.",
    },
    list: [
      ["Cooling-tower water swap with local farms", 0.88, 0.62, 19],
      ["Rain-to-rack storage for summer peaks", 0.74, 0.70, 12],
      ["Greywater credits for university campuses", 0.71, 0.55, 10],
    ],
  },
  {
    title: "Micro-grids for farm co-ops", short: "farm micro-grids", client: "Rural energy nonprofit",
    engines: ["Google", "Trends"], results: 38, clusters: 5, ideas: 22, kept: 6,
    why: {
      scout: "Trends shows search interest in co-op solar up 40% this year, so it joins the mix.",
      map: "Utility-run programs dominate. Co-op-owned storage is an open gap.",
      panel: "Feasibility liked shared batteries. Risk worried about interconnect delays.",
      decide: "Shared battery leasing ranks first at 0.81.",
    },
    list: [
      ["Shared battery leasing between neighboring farms", 0.81, 0.69, 12],
      ["Irrigation pumps as a demand-response fleet", 0.76, 0.73, 9],
      ["Grain-dryer heat recovery bundles", 0.70, 0.80, 7],
    ],
  },
  {
    title: "Wildfire smoke alerts for schools", short: "smoke alerts", client: "State education office",
    engines: ["News", "Scholar"], results: 49, clusters: 6, ideas: 18, kept: 5,
    why: {
      scout: "Air-quality research lives in Scholar, and live incidents in News.",
      map: "Consumer AQI apps are crowded. School-specific playbooks are rare.",
      panel: "Market and Risk agreed that liability makes automated recess calls attractive.",
      decide: "Recess-decision bot leads at 0.83.",
    },
    list: [
      ["Automatic recess calls from AQI sensors", 0.83, 0.78, 10],
      ["HVAC filter swap scheduling from forecasts", 0.75, 0.81, 8],
      ["Parent text alerts tied to bus routes", 0.68, 0.88, 6],
    ],
  },
];
