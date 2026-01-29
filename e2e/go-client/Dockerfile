# E2E test client using Go client library
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Copy the client library first
COPY clients/go /build/clients/go

# Copy the e2e test
COPY e2e/go-client /build/e2e/go-client

# Build the test binary
WORKDIR /build/e2e/go-client
RUN go mod tidy && go build -o /e2e-go-client .

# Final stage
FROM alpine:3.18
COPY --from=builder /e2e-go-client /e2e-go-client
CMD ["/e2e-go-client"]
