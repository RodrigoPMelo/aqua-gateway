# Variables
DOCKER_IMAGE_NAME=aqua-gateway
RAILWAY_SERVICE_ID=2819530b-4063-4c32-87bc-edbc7c942a83
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
	RAILWAY_TOKEN=$(RAILWAY_TOKEN) railway up --service $(RAILWAY_SERVICE_ID)

.PHONY: all build test docker-build deploy
