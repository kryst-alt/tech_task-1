# Lead Management and Automation System

- What we're building: `docs/PROJECT.md`. Read it before feature work.
- Work journal: `.claude/journal/<date>.md`. Append decisions, verification and open items at the end of each session.
- Stack and approach: `docs/STACK.md`. Follow it. Record any change of library or design there before coding against it.

## Token budget

Aim for quality output at the lowest token cost.

- Work inline. Spawn a subagent only when the user asks, or when a broad sweep would otherwise flood the main context.
- No multi-agent or multi-model fan-outs (arena, swarm, interrogate, parallel agents) unless the user asks.
- Read only what you need: grep first, read line ranges, never re-read a file you just wrote or already have.
- Batch independent tool calls in one turn.
- Test with the narrowest command that proves the change. Run the full suite once before calling a feature done.
- Load a skill only when the table below calls for it. Skip skills for trivial work (one-file edits, questions, config tweaks).
- Keep replies short: what changed, how it was verified, what's still open.
- Ask only when the decision is the user's. Otherwise pick a sensible default and say which one.

## Coding rules (Karpathy guidelines, always on)

1. Think first: state assumptions, and ask when the brief is ambiguous.
2. Simplicity: the minimum code that meets the requirement. No speculative abstractions or options.
3. Surgical changes: touch only what the task needs and match the existing style.
4. Goal-driven: turn each task into a check that proves it, then loop until it passes.

## Which skill for which job

Several overlapping skills are installed. Use the one listed and skip the others.

| Job | Skill | Use when |
|---|---|---|
| Open design question | `superpowers:brainstorming` | A feature has real unanswered design choices that the docs don't settle |
| Plan multi-step work | `superpowers:writing-plans` | The work spans several files or steps |
| Data model | `mattpocock-skills:domain-modeling` | Designing the lead schema and deal stages |
| Tests-first logic | `superpowers:test-driven-development` | Signature check, dedupe, email upsert, stage transitions |
| Debugging | `superpowers:systematic-debugging` | The cause isn't obvious after one look |
| Review | `mattpocock-skills:code-review` | Once per finished feature, not after every edit |
| Before saying "done" | `superpowers:verification-before-completion` | Every feature |
| User-facing prose | `no-ai-slop` | README, setup guide, PR descriptions |

These override `superpowers:using-superpowers`: don't check for skills on every message.

**Only when the user types the command:** pstack (`/poteto-mode`, `/how`, `/why`, `/architect`, `/arena`, `/interrogate`, `/swarm`, `/unslop` and the rest) and Matt Pocock's `/grill-me`, `/to-spec`, `/to-tickets`, `/implement`. Claude can't start these itself, and most of them fan out to several agents, so they cost a lot of tokens.

## Project rules

- Never commit secrets: signing secret, OAuth client secret, refresh tokens. Keep them in `.env`, which git ignores, and keep a `.env.example` up to date.
- Don't ask the user to sign in to Google again just to sync. Store the refresh token and refresh the access token automatically.
