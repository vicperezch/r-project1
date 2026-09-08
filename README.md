# Airline MCP Chatbot

A terminal chatbot in Go that acts as an MCP host. It talks to Claude over the Anthropic API, keeps
conversational context across turns, connects to several MCP servers at once, and logs every MCP
request and response. It ships with its own MCP server for an airline assistant, and can connect to
any other MCP server through a config file.

## Running

```
cp .env.example .env      # put your key in it
make workspace-init       # scratch git repo for the git MCP server
make compose-build
make chat                 # docker compose run --rm chatbot
```

Compose runs Postgres, the airline server over HTTP, and the chatbot, starting the first two and
waiting for both to be healthy. `make compose-reset` drops the database volume and re-seeds.

Without docker:

```
make db-up && make build
export ANTHROPIC_API_KEY=...
./bin/chatbot --servers configs/servers.example.json
```

Model defaults to `claude-haiku-4-5`, override with `CHATBOT_MODEL` or `--model`. To use someone
else's MCP server, copy a config and pass `--servers path.json`. The file uses the same `mcpServers`
shape as the Claude Desktop config, so a published config can be pasted in unchanged.

## Demo script

**Servers and tools.** `/servers` shows three connected, `/tools` shows 33.

**Context across turns.** Ask in sequence. The second answer is about Turing only because the first
exchange is resent with it. No tools involved.

```
who was Alan Turing?
when was he born?
```

**Use case 1 and 2, search and details.** City names work, IATA codes are not required.

```
what flights go from Guatemala City to Mexico City on 2026-09-14?
give me the details on the first one
```

Three flights, 06:00, 12:15 and 18:30. The 06:00 is 18 of 20 sold, 90 percent full, 2 economy seats
left and no business.

**Use case 3, irregularity.** Writes data, so the approval prompt appears.

```
that flight just got cancelled, weather at GUA. who is affected?
```

18 passengers in rebooking priority order: 2 platinum, 3 gold, 4 silver, 9 untiered.

**Use case 4, reassignment.** Ask for the plan, then commit it.

```
what are the options for rebooking them?
apply it
```

All 18 are placed: six on the 12:15, five on the 18:30, seven on the 06:00 next morning. Two of the
four business passengers keep business, the other two are downgraded, because waiting overnight for
a business seat costs 1440 minutes against 615 for downgrading now.

**Reference servers, then the log.**

```
read the README in /workspace and summarize the last two commits
/log 40
```

Run `make compose-reset` before demoing again, since the cancellation and reassignment are real
writes.

## Slash commands

`/help`, `/servers`, `/tools [server]`, `/log [n]`, `/history`, `/reset`, `/quit`.

## Architecture

```
terminal REPL          internal/chat    loop, slash commands, tool approval
  |
  +-- Anthropic API    internal/llm     streaming, history, context trimming
  |
  +-- MCP host         internal/host    connections, tool registry, dispatch
        |                               every call recorded by internal/mcplog
        +-- airline server (this repo, stdio or http)
        +-- filesystem and git servers (Anthropic reference, stdio)
        +-- any other server in the config
```

Tools are discovered at runtime and namespaced as `server__tool`, so two servers can export the same
name. Each server's JSON Schema is converted into an Anthropic tool definition. Nothing in the
chatbot is specific to the airline server, which is what lets it talk to other people's servers.

The airline server speaks stdio and streamable HTTP behind `-transport`. Over HTTP it runs as its own
compose service and anyone on the network can reach `http://host:8080/mcp`.

## Airline tools

| Tool | Use case | Writes |
| --- | --- | --- |
| `search_flights` | 1, city names or IATA codes | no |
| `get_flight_details` | 2, seats per cabin and load factor | no |
| `get_booking` | one PNR | no |
| `cancel_flight` | 3, returns everyone stranded | yes |
| `list_affected_passengers` | 3, read only view of the same list | no |
| `find_reassignment_options` | 4, proposes a plan with costs | no |
| `apply_reassignment` | 4, commits it | yes |

Propose and apply are separate so the model can show a rep the plan before anything is committed.

## Reassignment

Passengers are served in priority order (loyalty tier, then booking date, then PNR as a
deterministic tie-break). Each takes the cheapest seat still free when their turn comes:

```
cost = delayMinutes + 240 if downgraded from business to economy
```

Delay is measured against the original arrival. Pricing a downgrade at 240 means a business
passenger accepts economy rather than wait more than four hours, but waits for anything less. Direct
flights on the same route within 48 hours only, and economy is never upgraded into free business.

Greedy, not globally optimal, on purpose: it is deterministic and can be explained to a passenger at
the counter.

## Tool approval

Read-only tools run automatically. Tools that write print the call and wait for y/n. `--yes` skips
the prompt.

Whether a tool writes is decided from its name, matched on whole segments so `get_settings` does not
trip on `set`. MCP annotations can only add confirmation, never remove it: the spec says a client
must not make tool use decisions from annotations sent by an untrusted server, and connecting to
other people's servers is the point. Declining goes back to the model as an error result, so it
explains itself instead of silently retrying.

## Interaction log

`logs/mcp-<timestamp>.jsonl`, one object per line, two levels in one timeline:

- `protocol`, from wrapping each transport in `mcp.LoggingTransport`, so every JSON-RPC frame is
  captured including `initialize`, `tools/list` and the server's own requests back to the client.
  Responses are named after the request they answer.
- `call`, written by the host, carrying what the frames do not: server, tool, duration, truncation.
  A request and its response share a `seq`.

```
sudo jq -c 'select(.level=="call")' logs/*.jsonl
```

## Context window

Haiku 4.5 has a 200K window, not 1M, and a single `read_file` can be large. Tool results are capped
at 8 KB with the truncation stated in the result. History is measured before every request: a local
estimate first, and `count_tokens` only once that estimate says the history is large, so an ordinary
turn costs no extra round trip. Over budget, the oldest exchanges are dropped whole. Cuts happen
only at the start of a user turn, never at one carrying tool results, so a `tool_use` is never
separated from its `tool_result`. `--context-tokens` lowers the budget to watch it work.

## Testing

```
make test                    # no database needed
make db-up && make test-db   # plus integration tests
go test -race ./...
```

176 tests across 11 packages. Most need no database, API key or network: the optimizer is a pure
function, the host is tested against a real stub MCP server over `httptest`, and the agent loop is
driven by a scripted model. Tests needing Postgres skip when `DATABASE_URL` is unset, and those that
write restore the data afterwards.

## Layout

```
cmd/chatbot          chatbot and MCP host
cmd/airline-mcp      airline MCP server
internal/config      servers config loading
internal/llm         Anthropic client, history, trimming
internal/host        MCP connections, registry, dispatch
internal/mcplog      JSONL interaction log
internal/chat        REPL, commands, agent loop
internal/airline     domain, store, services, MCP tools
db/init              schema and seed SQL
deploy               dockerfiles
```
