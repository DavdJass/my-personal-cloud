.PHONY: help deps frontend build build-pi run dev clean

BIN := my-personal-cloud
PKG := ./cmd/server

help:
	@echo "Targets:"
	@echo "  deps       Install Go modules and frontend deps"
	@echo "  frontend   Build the React frontend (web/dist)"
	@echo "  build      Build the server for the host platform"
	@echo "  build-pi   Cross-compile for Raspberry Pi (linux/arm64)"
	@echo "  run        Build frontend + server and run locally"
	@echo "  dev        Run Go server (8080) and Vite dev (5173) - 2 terminals"

deps:
	go mod tidy
	cd web && npm install

frontend:
	cd web && npm run build

build: frontend
	go build -o $(BIN) $(PKG)

build-pi: frontend
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN)-arm64 $(PKG)

run: build
	./$(BIN)

dev:
	@echo "Run in two terminals:"
	@echo "  1) go run $(PKG)"
	@echo "  2) cd web && npm run dev"

clean:
	rm -f $(BIN) $(BIN)-arm64
	rm -rf web/dist/*
