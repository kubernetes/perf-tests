# Use a minimal base image with Go support
FROM golang:1.23 AS builder

# Set working directory
WORKDIR /app

# Copy Go modules and install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the application code
COPY . .

RUN apt update && apt install wireguard -y 

# Build the WireGuard exporter binary
RUN go build -o wg_exporter main.go

# Expose the Prometheus metrics port
EXPOSE 8080

# Run the exporter
CMD ["/app/wg_exporter"]
 