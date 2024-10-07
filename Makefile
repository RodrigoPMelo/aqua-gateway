# Variables
DOCKER_IMAGE_NAME=aqua-gateway
RAILWAY_PROJECT_ID=0afa8246-fb4a-4981-9ef3-6c72b6e328dd
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
	railway link -s $(RAILWAY_PROJECT_ID)
	railway up

.PHONY: all build test docker-build deploy
