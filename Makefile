# Variables
DOCKER_IMAGE_NAME=aqua-gateway
RAILWAY_SERVICE_ID=e19f9dc6-d325-4f22-81bd-13e6f6e98353
RAILWAY_ENVIRONMENT=8b7d1bfc-ee25-42a8-8e03-21fccc7f7191

# Default target
all: build

# Build the Go app
build:
	go build -o ./bin/aqua-gateway

# Docker build
docker-build:
	docker build -t $(DOCKER_IMAGE_NAME) .

# Deploy to Railway using Railway CLI
deploy: docker-build
	RAILWAY_TOKEN=$(RAILWAY_TOKEN) railway up --environment $(RAILWAY_ENVIRONMENT) --service $(RAILWAY_SERVICE_ID)

.PHONY: all build test docker-build deploy
