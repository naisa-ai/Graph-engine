# E2E test client using Python client library
FROM python:3.11-slim

WORKDIR /app

# Install dependencies
RUN pip install --no-cache-dir grpcio>=1.60.0 protobuf>=4.0.0

# Copy the client library
COPY clients/py /app/clients/py

# Generate proto files for the client
WORKDIR /app/clients/py
RUN pip install grpcio-tools && make proto || true

# Copy the e2e test
WORKDIR /app
COPY e2e/py-client /app/e2e/py-client

# Run the test
WORKDIR /app/e2e/py-client
CMD ["python3", "test_e2e.py"]
