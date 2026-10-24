# Frontend design brief

Source notes from the team, plus how they map to the code.

## Notes

- Modern corporate aesthetic
- Dashboard focus
- Use an existing UI library: [Ant Design](https://ant.design/)
- References:
  - Dots (OpenAI agent platform) – https://openai.com/index/introducing-dots/
  - Muse (Meta agent platform)
  - Apple Shortcuts
- Use taste skills / awesome design resources

## How this is applied

| Idea | Where |
|------|-------|
| Ant Design v6 + App Router SSR styles | `src/app/providers.tsx` (`AntdRegistry` + `ConfigProvider`) |
| Corporate theme tokens (neutral greys, one indigo accent, soft radii) | `src/theme/theme.ts` |
| Dashboard shell: sidebar nav + top bar | `src/components/AppShell.tsx`, `src/app/(dashboard)/layout.tsx` |
| Agent-platform pages (Dots / Muse) | `/` overview, `/agents`, `/runs` |
| Apple Shortcuts-style colorful action tiles | `/workflows`, `src/components/WorkflowTile.tsx` |

## Principles

- Calm, neutral surfaces; color only for status and workflow tiles.
- Generous whitespace, 8px spacing grid, 12px card radius.
- Ant Design components first; Tailwind only for small layout tweaks.
- Placeholder data lives in `src/lib/mock-data.ts` until backend endpoints exist.
