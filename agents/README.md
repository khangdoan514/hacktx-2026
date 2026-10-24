# NMC3 agent runtime

This directory is the home of the **general-purpose Go agent and orchestration service**. It exposes versioned APIs that can be used by different applications and workflows. The existing Next.js website in `frontend/` is the first integrated client. The documentation covers the intended product end to end, while the current Go package contains only the first pass's shared contracts.

## Product direction

- [End-to-end agent product specification](docs/product-spec.md)
- [Target architecture and execution lifecycle](docs/target-architecture.md)
- [Eight-pass product specifications](docs/pass-specs.md)
- [Storage decision and data model](docs/storage.md)
- [Testing and coding-agent workflow](docs/testing.md)

## First delivery slice

- [First pass scope and acceptance](docs/first-pass.md)
- [Agent architecture and contracts](docs/architecture.md)
- [Delivery roadmap](docs/plan.md)
- [Parallel work packages and agent handoffs](docs/parallel-work.md)
- [Shared Go and HTTP contracts v1](docs/contracts-v1.md)

## Current status

The shared Go types, interfaces, and example HTTP payloads exist. No agent runtime, model adapter, tool gateway, or agent API is implemented yet. The current Next.js website and FastAPI backend are starter scaffolds. The website will integrate with the Go service through its public API; whether FastAPI serves as a thin proxy remains an integration decision.

## Guiding rules

- A depth-0 orchestrator may invoke authorized depth-1 specialists. Specialists cannot invoke other agents.
- Every agent invocation has explicit input, output, tool permissions, limits, and trace identity.
- The runtime, rather than a model prompt, enforces delegation and tool permissions.
- Checked-in fictional data and a deterministic adapter make the first pass testable; the configured provider path supplies live agent behavior.
