.PHONY: help lint test build run swagger deploy clean install-tools

# Variables
APP_NAME=vcs-notification
BUILD_DIR=./bin
MAIN_PATH=./cmd/server
DOCKER_IMAGE=vcs-notification:latest
GO=go
GOLANGCI_LINT=golangci-lint
SWAG=swag

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install-tools: ## Install development tools
	@echo "Installing tools..."
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@$(GO) install github.com/swaggo/swag/cmd/swag@latest
	@echo "Tools installed successfully"

lint: ## Run linters
	@echo "Running linters..."
	@$(GOLANGCI_LINT) run --timeout=5m ./...

test: ## Run tests
	@echo "Running tests..."
	@$(GO) test -v -race -coverprofile=coverage.out ./...
	@$(GO) tool cover -html=coverage.out -o coverage.html

test-crud-integration: ## Run CRUD integration tests against CloudSQL database
	@echo "Running integration tests..."
	@echo "Connecting to CloudSQL database..."
	@$(GO) test -v ./tests/integration/... -timeout 60s

build: ## Build the application
	@echo "Building application..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

run: ## Run the application
	@echo "Running application..."
	@$(GO) run $(MAIN_PATH)

swagger: ## Generate Swagger documentation
	@echo "Generating Swagger docs..."
	@$(SWAG) init -g cmd/server/main.go -o docs
	@echo "Swagger docs generated in ./docs"

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "Docker image built: $(DOCKER_IMAGE)"

docker-run: ## Run Docker container
	@echo "Running Docker container..."
	@docker run --rm -p 8080:8080 --name $(APP_NAME) $(DOCKER_IMAGE)

deploy: build docker-build ## Build and create Docker image
	@echo "Deployment artifacts ready"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@rm -rf docs
	@echo "Clean complete"

tidy: ## Tidy go modules
	@echo "Tidying go modules..."
	@$(GO) mod tidy

fmt: ## Format code
	@echo "Formatting code..."
	@$(GO) fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	@$(GO) vet ./...

check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)

dev: ## Run in development mode with hot reload (requires air)
	@echo "Starting development server..."
	@air || $(GO) run $(MAIN_PATH)

.DEFAULT_GOAL := help