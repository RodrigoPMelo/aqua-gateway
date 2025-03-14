# Stage 1: Build the Go app
FROM golang:1.23.2 as builder

WORKDIR /app

# Copy go.mod and go.sum files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

RUN mkdir /data

# Copy the source code
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -o /aqua-gateway

# Stage 2: Create a minimal image to run the Go application
FROM alpine:latest

WORKDIR /root

# Install ca-certificates to enable HTTPS for Firebase
RUN apk add --no-cache ca-certificates

COPY --from=builder --chown=nonroot:nonroot /data /

# Copy the Go app from the builder stage
COPY --from=builder /aqua-gateway .
COPY firebase_service_account.json /root/firebase_service_account.json

# Run the Go app
CMD ["./aqua-gateway"]
