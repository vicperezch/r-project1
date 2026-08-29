# Airline MCP Chatbot

A terminal chatbot written in Go that acts as an MCP host. It talks to Claude over the Anthropic
API, keeps conversational context across turns, connects to several MCP servers at the same time,
and logs every MCP request and response.

It ships with its own MCP server for an airline assistant, intended for counter and call center
reps, and it can connect to any other MCP server through a config file.

## Requirements coverage

| Requirement | Where it lives |
| --- | --- |
| 1. Chatbot connected to an LLM via API | `internal/llm`, Anthropic Go SDK |
| 2. Conversation context is maintained | `internal/chat`, full message history resent every turn |
| 3. Log of all MCP requests and responses | `internal/mcplog`, JSONL per run under `logs/` |
| 4. Uses the Anthropic git and filesystem MCP servers | `configs/servers.example.json` |
| 5. A local MCP server used by the chatbot | `cmd/airline-mcp`, `internal/airline` |

## Airline server use cases

1. Search for flights between two cities.
2. Get the details of a specific flight.
3. Handle an irregularity: a flight is cancelled and the system finds every affected passenger.
4. Reassign those passengers to new flights, optimizing the outcome.

## Architecture

```
terminal REPL
   |
   |  chat loop, slash commands, tool approval      internal/chat
   |
   +-- Anthropic Messages API                       internal/llm
   |
   +-- MCP host                                     internal/host
         |  connection manager, tool registry, dispatch
         |  every call recorded by                  internal/mcplog
         |
         +-- airline MCP server (this repo, stdio or http)
         +-- filesystem MCP server (Anthropic reference, stdio)
         +-- git MCP server (Anthropic reference, stdio)
         +-- any other MCP server listed in the config
```

The host discovers tools at runtime with `tools/list`, namespaces them as `server__tool` so two
servers can export the same tool name, and converts each server's JSON Schema into an Anthropic
tool definition. Nothing about the chatbot is specific to the airline server, which is what lets it
talk to MCP servers written by other people.

## Layout

```
cmd/chatbot          terminal chatbot, MCP host
cmd/airline-mcp      airline MCP server, stdio or streamable http
internal/config      servers config and environment loading
internal/llm         Anthropic client wrapper and history
internal/host        MCP connections, tool registry, dispatch
internal/mcplog      JSONL interaction log
internal/chat        REPL, slash commands, agent loop
internal/airline     domain, postgres store, services, MCP tools
db/init              schema and seed SQL, run by postgres on first boot
configs              MCP server config files
deploy               dockerfiles
```

## Dependencies

- `github.com/anthropics/anthropic-sdk-go` for the Claude API
- `github.com/modelcontextprotocol/go-sdk` for MCP client and server
- `github.com/jackc/pgx/v5` for Postgres

## Running

Build both binaries:

```
make build
```

Full instructions arrive with the docker compose stack. The intended entry points are
`docker compose run --rm chatbot` for the containerized stack, and `./bin/chatbot` against a
locally running Postgres for development.

The chatbot reads `ANTHROPIC_API_KEY` from the environment. The model defaults to
`claude-haiku-4-5` and can be changed with `CHATBOT_MODEL` or `--model`.

To point the chatbot at a different set of MCP servers, copy `configs/servers.example.json`, edit
it, and pass `--servers path/to/your.json`. The file uses the same `mcpServers` shape as the Claude
Desktop config, so a server config published by someone else can be pasted in unchanged.

## Implementation status

Built in small commits, in this order.

1. Scaffold: module, layout, makefile, gitignore
2. Airline domain types, SQL schema and seed data
3. Postgres store with flight search and details
4. Airline MCP server over stdio with search and details tools
5. Flight cancellation and affected passenger tools
6. Reassignment optimizer with propose and apply tools
7. Streamable HTTP transport and healthcheck
8. Anthropic client and REPL with conversation context
9. MCP connection manager, tool registry and namespacing
10. JSONL interaction log and the `/log` command
11. Agent tool-use loop with approval and slash commands
12. Context trimming and tool result truncation
13. Dockerfiles and compose stack
14. Optimizer and tool registry unit tests
15. Full docs and demo script

Current status: step 1.
