.PHONY: build chatbot airline test fmt vet tidy clean run-chatbot run-airline

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
