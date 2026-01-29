# E2E test client using Go client library
ARG BASE_IMAGE=graph-engine-e2e-go-base:latest
FROM ${BASE_IMAGE} AS builder

WORKDIR /build

# Copy the client library first
COPY clients/go /build/clients/go

# Copy the e2e test
COPY e2e/go-client /build/e2e/go-client

# Build the test binary
WORKDIR /build/e2e/go-client
RUN go mod tidy && go build -o /e2e-go-client .

# Final stage
FROM alpine:3.21
RUN apk --no-cache add ca-certificates
COPY --from=builder /e2e-go-client /e2e-go-client
CMD ["/e2e-go-client"]
