// Package graphengine provides a Go client library for the Graph-engine service.
package graphengine

import (
	"crypto/tls"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Option configures the client.
type Option func(*clientOptions)

type clientOptions struct {
	timeout       time.Duration
	dialOptions   []grpc.DialOption
	metadata      map[string]string
	maxRetries    int
	retryBackoff  time.Duration
	maxRetryDelay time.Duration
}

func defaultOptions() *clientOptions {
	return &clientOptions{
		timeout:       30 * time.Second,
		dialOptions:   []grpc.DialOption{},
		metadata:      make(map[string]string),
		maxRetries:    3,
		retryBackoff:  100 * time.Millisecond,
		maxRetryDelay: 5 * time.Second,
	}
}

// WithTimeout sets the default timeout for RPC calls.
func WithTimeout(d time.Duration) Option {
	return func(o *clientOptions) {
		o.timeout = d
	}
}

// WithInsecure disables TLS for the connection.
func WithInsecure() Option {
	return func(o *clientOptions) {
		o.dialOptions = append(o.dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
}

// WithTLS enables TLS with the provided configuration.
func WithTLS(config *tls.Config) Option {
	return func(o *clientOptions) {
		o.dialOptions = append(o.dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(config)))
	}
}

// WithTLSFromFile loads TLS credentials from certificate files.
func WithTLSFromFile(certFile, keyFile, caFile string) Option {
	return func(o *clientOptions) {
		// Load client cert if provided
		var certs []tls.Certificate
		if certFile != "" && keyFile != "" {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err == nil {
				certs = append(certs, cert)
			}
		}

		config := &tls.Config{
			Certificates: certs,
		}

		o.dialOptions = append(o.dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(config)))
	}
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, backoff time.Duration) Option {
	return func(o *clientOptions) {
		o.maxRetries = maxRetries
		o.retryBackoff = backoff
	}
}

// WithMaxRetryDelay sets the maximum delay between retries.
func WithMaxRetryDelay(d time.Duration) Option {
	return func(o *clientOptions) {
		o.maxRetryDelay = d
	}
}

// WithMetadata adds metadata to all RPC calls.
// Common use: tenant ID, correlation ID, etc.
func WithMetadata(md map[string]string) Option {
	return func(o *clientOptions) {
		for k, v := range md {
			o.metadata[k] = v
		}
	}
}

// WithDialOptions adds custom gRPC dial options.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *clientOptions) {
		o.dialOptions = append(o.dialOptions, opts...)
	}
}

// WithBlock makes the dial block until the connection is established.
func WithBlock() Option {
	return func(o *clientOptions) {
		o.dialOptions = append(o.dialOptions, grpc.WithBlock())
	}
}

// outgoingMetadata returns gRPC metadata from the client options.
func (o *clientOptions) outgoingMetadata() metadata.MD {
	if len(o.metadata) == 0 {
		return nil
	}
	md := metadata.New(o.metadata)
	return md
}
