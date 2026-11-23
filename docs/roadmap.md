# Roadmap and Architecture

This document defines the package layout and phased implementation plan for `nand`.

## Package Layout

Keep the core contracts in the root package so external domain repositories can import `nand` and register tools without depending on the built-in coding tools or the CLI.

```text
nand/
├── go.mod
├── message.go          # Neutral Message, ToolCall, Usage, opaque ProviderData
├── tool.go             # Tool interface, PermissionLevel, Registry
├── provider.go         # Provider interface (Complete)
├── permission.go       # Approver interface (permission gates)
├── loop.go             # Agent loop, turn limit, output truncation, hooks
├── trace.go            # JSONL session logger and token/cost tracker
├── provider/
│   └── openai/         # OpenAI-compatible HTTP client (backoff on 429/503, opaque fields)
├── tools/              # Built-in coding tools (read, write, edit, bash)
└── cmd/
    └── nand/           # Thin CLI (env config, terminal approver, REPL / one-shot)
```

### Design Principles

- **Standard library first.** Use `net/http`, `encoding/json`, and `context` before adding any third-party dependency.
- **Neutral core.** Provider-specific fields (such as Gemini `thought_signature` or raw `tool_calls`) are stored as opaque data and echoed back untouched by the provider adapter.
- **Single contract for external tools.** Domain packages live in separate repositories and integrate solely through the `Tool` interface (`Name`, `Description`, `Schema`, `Permission`, `Run`).

## Development Phases

Each phase adds one core concept and ends with a runnable, tested milestone.

### Phase 1 — Skeleton and Provider Client (Stages 0–1)

**Goal:** Send a system prompt and user prompt to an OpenAI-compatible endpoint and print the reply.

- Initialize `go.mod` and document build/test commands in `AGENTS.md`.
- Define neutral `Message`, `ToolCall`, `Usage`, and `Provider` types.
- Implement `provider/openai` with configurable base URL, API key, and model, plus exponential backoff (`2, 4, 8, 16s`) for `429` and `503` responses.
- Build a minimal one-shot CLI in `cmd/nand`.
- **Exit criteria:** One-shot prompt works against Gemini via the OpenAI-compatible endpoint; `httptest.Server` unit tests cover JSON serialization, opaque field preservation, and retries.

### Phase 2 — Tool Interface and Agent Loop (Stages 2–3)

**Goal:** Turn a single completion call into a bounded agent loop capable of invoking tools. This phase stabilizes the `Tool` interface for external domain repos.

- Define `Tool`, `PermissionLevel`, and `Registry` in `tool.go`.
- Implement the `Agent` loop in `loop.go` with a hard `MaxTurns` cap.
- Support multiple `tool_calls` returned in a single assistant turn and preserve raw `tool_calls` untouched in message history.
- Add the first built-in tool: `tools/read` (bounded file reader).
- **Exit criteria:** The agent can inspect a local file using `read` across multiple turns and stop cleanly; `Tool` interface is stable for external consumers.

### Phase 3 — Coding Tools and Permission Gates (Stages 4 and 6)

**Goal:** Add mutating tools behind explicit permission checks so the agent is safe from day one.

- Implement the `Approver` gate in `permission.go` and wire it into the loop: auto-allow read-only tools, prompt for write/exec tools, and return denials to the model as tool results.
- Implement `tools/write`, `tools/edit`, and `tools/bash` (with execution timeout).
- **Exit criteria:** The agent can fix a small bug in a sample Go package and run `go test`, requesting user confirmation before writing files or running shell commands.

### Phase 4 — Context, Cost, and Observability (Stage 5)

**Goal:** Make context growth and cost transparent and keep long sessions bounded.

- Truncate oversized tool outputs with a clear truncation marker.
- Track per-turn and cumulative token usage and estimated cost.
- Write JSONL session traces (`trace.go`) recording each turn, tool call, latency, and usage.
- Add basic context compaction when approaching a configured token budget.
- **Exit criteria:** Multi-turn runs emit a complete JSONL trace and print turn-by-turn token and cost summaries.

### Phase 5 — Multi-Provider Verification and External Integration

**Goal:** Prove provider and domain independence.

- Verify `nand` against a second OpenAI-compatible provider (OpenRouter or DeepSeek) using only configuration changes.
- Verify external domain tools register cleanly via the `Tool` interface without modifying `nand`.
- Use JSONL traces from Phase 4 to support comparative evaluations (model vs. harness).

## Parallel Work Split

- **During Phases 1–2:** One contributor builds `nand` Phases 1–2 while another can work independently on external domain clients/CLIs that do not yet import `nand`.
- **After Phase 2:** One contributor owns Phase 4 (context, cost, JSONL tracing, evaluation) while another owns Phase 3 (`write`/`edit`/`bash` tools and permission gates) and external tool wiring.
