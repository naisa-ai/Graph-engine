# Build stage - uses cached base image if available
ARG BASE_IMAGE=graph-engine-e2e-go-base:latest
FROM ${BASE_IMAGE} AS builder

WORKDIR /app

# Copy the entire project for the replace directive
COPY . /app/graph-engine/

# Build the e2e client
WORKDIR /app/graph-engine/e2e/client

# Download dependencies (fast if base has them cached)
RUN go mod download

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /e2e-client main.go

# Runtime stage
FROM alpine:3.21

# Install ca-certificates for TLS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary
COPY --from=builder /e2e-client /app/e2e-client

# Run the client
ENTRYPOINT ["/app/e2e-client"]
