# E2E test client using Python client library
ARG BASE_IMAGE=graph-engine-e2e-py-base:latest
FROM ${BASE_IMAGE}

WORKDIR /app

# Copy the client library
COPY clients/py /app/clients/py

# Generate proto files for the client
WORKDIR /app/clients/py
RUN make proto || true

# Copy the e2e test
WORKDIR /app
COPY e2e/py-client /app/e2e/py-client

# Run the test
WORKDIR /app/e2e/py-client
CMD ["python3", "test_e2e.py"]
