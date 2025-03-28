# WireGuard Prometheus Exporter

## Overview
This is a Prometheus exporter for WireGuard that collects and exposes metrics about WireGuard peers and allowed IPs. The exporter periodically polls the WireGuard interface and provides metrics that can be scraped by Prometheus.

## Features
- Exposes the total number of WireGuard peers.
- Exposes the total number of allowed IPs across all peers.
- Configurable polling interval and network interface.
- Provides metrics over HTTP for Prometheus scraping.

## Installation
### Prerequisites
- Go 1.18 or later
- WireGuard installed and configured
- Prometheus for scraping metrics

### Build from Source
```sh
git clone git@github.com:Azure/perf-tests.git
cd network/tools/wireguard-scale/collector
go build -o wireguard-exporter
```

## Usage
### Running the Exporter
```sh
./wireguard-exporter -ifname cilium_wg0 -address 0.0.0.0 -port 8080 -poll 60
```

### Command-line Flags
| Flag         | Default       | Description                              |
|-------------|-------------|------------------------------------------|
| `-ifname`   | `cilium_wg0` | Name of the WireGuard interface         |
| `-address`  | `0.0.0.0`    | Address to bind the HTTP server         |
| `-port`     | `8080`       | Port to expose metrics                  |
| `-poll`     | `60`         | Polling interval in seconds             |

## Metrics Exposed
The exporter exposes the following Prometheus metrics:

| Metric Name                  | Type  | Description                                  |
|------------------------------|------|----------------------------------------------|
| `wireguard_peers_total`       | Gauge | Total number of WireGuard peers             |
| `wireguard_allowed_ips_total` | Gauge | Total number of allowed IPs across all peers |

### Accessing Metrics
Once the exporter is running, metrics can be accessed at:
```
http://<address>:<port>/metrics
```
Example:
```
http://localhost:8080/metrics
```

## Example Prometheus Configuration
To configure Prometheus to scrape metrics from this exporter, add the following job to your `prometheus.yml`:
```yaml
scrape_configs:
  - job_name: 'wireguard_exporter'
    static_configs:
      - targets: ['localhost:8080']
```

