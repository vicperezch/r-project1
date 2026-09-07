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
