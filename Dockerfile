# syntax=docker/dockerfile:1.4

FROM golang:1.24-bookworm AS builder

WORKDIR /app

# install gstreamer and dependencies
RUN apt-get update && apt-get install -y \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-x \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    libgstreamer-plugins-bad1.0-dev \
    pkg-config \
    iproute2 \
    && rm -rf /var/lib/apt/lists/*


COPY . .

############################
# Stage 1: Lint            #
############################
FROM builder AS lint

WORKDIR /app

# Install golangci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.1.6

# Run linting with necessary environment
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/.cache/golangci-lint \
    $(go env GOPATH)/bin/golangci-lint run --timeout 10m0s ./...

############################
# Stage 2: Unit Tests      #
############################
FROM builder AS unit-test

WORKDIR /app
COPY . .

# Run tests with coverage reporting
RUN --mount=type=cache,target=/root/.cache/go-build \
    go test -v -race -coverprofile=coverage.txt -covermode=atomic ./... && \
    go tool cover -func=coverage.txt

############################
# Stage 3: Build           #
############################
FROM builder AS build

# Only build if linting and tests pass
COPY --from=lint /app/go.mod /app/go.mod
COPY --from=unit-test /app/coverage.txt /app/coverage.txt

RUN go mod download \
    && go mod verify \
    && go mod tidy \
    && go build -o app

############################
# Stage 4: Runtime Image   #
############################
FROM debian:bookworm-slim

ENV GST_PLUGIN_SCANNER=/usr/lib/x86_64-linux-gnu/gstreamer-1.0/gst-plugin-scanner

# Install GStreamer runtime dependencies
RUN apt-get update && apt-get install -y \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-x \
    iproute2 \
    && rm -rf /var/lib/apt/lists/*

# Install `slow-network`
WORKDIR /tmp
RUN apt-get update && apt-get install -y git && \
    git clone https://github.com/j1elo/slow-network.git && \
    cd slow-network && \
    chmod +x slow && \
    cp slow /usr/local/bin/ && \
    cd .. && \
    rm -rf slow-network && \
    apt-get remove -y git && apt-get autoremove -y

WORKDIR /app

# Copy binary from builder stage
COPY --from=build /app/app .

# Run the application
CMD ["./app"]