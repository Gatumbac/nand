# AGENTS.md

## Project

nand is a small agent harness written in Go. The goal is to understand and explain how agents work: the agent loop, tool calling, permissions, context and logging. It is domain independent.

## Rules

- Use the Go standard library first. Ask before adding a dependency.
- Nothing in this repo may import or mention Canvas or ESPOL. Domain code lives in other repos and registers tools through the Tool interface (name, description, JSON schema, handler, permission level).
- Talk to providers through OpenAI compatible chat completions, with the base URL, key and model configurable. Gemini first, then OpenRouter and DeepSeek. Provider quirks stay inside the provider adapter.
- Keep a neutral internal message type. Store provider specific fields (for example the Gemini `thought_signature`) as opaque data and send them back untouched. Return `tool_calls` exactly as received.
- Retry 503 and 429 responses with exponential backoff (2, 4, 8 and 16 seconds).
- Always cap the number of turns in the agent loop.
- Read secrets from the environment. Never log or commit them. Run commands that need them with `dotenv -e .env -- <cmd>`.

## Conventions

- English for code, identifiers, docs, commits and tests. Spanish only for strings an end user reads.
- Conventional commits, for example `feat(loop): add turn limit`.
- No comments unless they explain a non obvious reason. If a comment explains what the code does, rewrite the code.

## Commands

None yet. Add the exact build, test and lint commands here as soon as they exist.
