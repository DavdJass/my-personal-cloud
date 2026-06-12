.PHONY: help deps frontend build build-pi run dev clean deploy-pi

BIN := my-personal-cloud
PKG := ./cmd/server

help:
	@echo "Targets:"
	@echo "  deps         Install Go modules and frontend deps"
	@echo "  frontend     Build the React frontend (web/dist)"
	@echo "  build        Build the server for the host platform"
	@echo "  build-pi     Cross-compile for Raspberry Pi (linux/arm64)"
	@echo "  deploy-pi    One-command build and deploy to Raspberry Pi"
	@echo "  run          Build frontend + server and run locally"
	@echo "  dev          Run Go server (8080) and Vite dev (5173) - 2 terminals"

deps:
	go mod tidy
	cd web && npm install

frontend:
	cd web && npm run build

build: frontend
	go build -o $(BIN) $(PKG)

build-pi: frontend
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN)-arm64 $(PKG)

deploy-pi:
	@echo "======================================="
	@echo "  One-click deploy to Raspberry Pi"
	@echo "======================================="
	@echo ""
	@echo "First, copy deploy/.env.pi.example to deploy/.env.pi"
	@echo "and edit it with your Pi's address and credentials:"
	@echo ""
	@echo "  cp deploy/.env.pi.example deploy/.env.pi"
	@echo "  nano deploy/.env.pi"
	@echo ""
	@echo "Then run the deployment script:"
	@echo ""
	@echo "  Linux/Mac:  make deploy-pi-run"
	@echo "  Windows:    .\deploy\deploy.ps1"
	@echo ""
	@echo "Or pass everything inline:"
	@echo ""
	@echo "  Linux/Mac:  ./deploy/deploy.sh -h 192.168.1.100 -u admin -p mypass"
	@echo "  Windows:    .\deploy\deploy.ps1 -PiHost 192.168.1.100 -AdminPass mypass"
	@echo "======================================="

deploy-pi-run: build-pi
	@test -f deploy/.env.pi || { echo "Create deploy/.env.pi first"; exit 1; }
	./deploy/deploy.sh -c deploy/.env.pi

run: build
	./$(BIN)

dev:
	@echo "Run in two terminals:"
	@echo "  1) go run $(PKG)"
	@echo "  2) cd web && npm run dev"

clean:
	rm -f $(BIN) $(BIN)-arm64
	rm -rf web/dist/*
