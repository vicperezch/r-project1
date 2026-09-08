.PHONY: build chatbot airline test fmt vet tidy clean run-chatbot run-airline run-airline-http

BIN := bin

build: chatbot airline

chatbot:
	go build -o $(BIN)/chatbot ./cmd/chatbot

airline:
	go build -o $(BIN)/airline-mcp ./cmd/airline-mcp

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN)

run-chatbot: chatbot
	./$(BIN)/chatbot

run-airline: airline
	./$(BIN)/airline-mcp -transport stdio

run-airline-http: airline
	DATABASE_URL=$(DATABASE_URL) ./$(BIN)/airline-mcp -transport http -addr :8080

# Local Postgres for development and the store integration tests. Superseded by
# docker compose once the full stack lands. Docker needs sudo on this machine.
PG_IMAGE  := postgres:17-alpine
PG_NAME   := airline-pg
DATABASE_URL ?= postgres://airline:airline@localhost:5432/airline

.PHONY: db-up db-down db-reset db-psql test-db

db-up:
	sudo docker run -d --rm --name $(PG_NAME) -p 5432:5432 \
		-e POSTGRES_USER=airline -e POSTGRES_PASSWORD=airline -e POSTGRES_DB=airline \
		-v "$(PWD)/db/init:/docker-entrypoint-initdb.d:ro" $(PG_IMAGE)
	@echo "waiting for postgres"
	@until sudo docker exec $(PG_NAME) pg_isready -U airline -d airline >/dev/null 2>&1; do sleep 1; done
	@echo "ready: $(DATABASE_URL)"

db-down:
	-sudo docker stop $(PG_NAME)

db-reset: db-down db-up

db-psql:
	sudo docker exec -it $(PG_NAME) psql -U airline -d airline

test-db:
	DATABASE_URL=$(DATABASE_URL) go test -v ./internal/airline/store/... ./internal/airline/mcpserver/...

# Docker compose stack. Docker needs sudo on this machine, and sudo strips the
# environment, so the API key is read from .env rather than the shell.
.PHONY: compose-build compose-up compose-down compose-reset compose-logs chat workspace-init

compose-build:
	sudo docker compose build

compose-up:
	sudo docker compose up -d postgres airline-mcp

compose-down:
	sudo docker compose down

# Drops the database volume so db/init runs again.
compose-reset:
	sudo docker compose down -v
	sudo docker compose up -d postgres airline-mcp

compose-logs:
	sudo docker compose logs -f airline-mcp

# The chatbot is an interactive REPL, so it needs run rather than up.
chat:
	sudo docker compose run --rm chatbot

# The git MCP server needs a real repository to read. This is a scratch repo,
# separate from the project history.
workspace-init:
	mkdir -p workspace
	git -C workspace rev-parse --git-dir >/dev/null 2>&1 || ( \
		git -C workspace init -q && \
		printf 'Demo workspace for the filesystem and git MCP servers.\n' > workspace/README.md && \
		git -C workspace add README.md && \
		git -C workspace -c user.name=demo -c user.email=demo@example.com commit -qm "initial commit" && \
		printf 'A second line, so git log has something to show.\n' >> workspace/README.md && \
		git -C workspace add README.md && \
		git -C workspace -c user.name=demo -c user.email=demo@example.com commit -qm "extend the readme" )
	@echo "workspace ready"
