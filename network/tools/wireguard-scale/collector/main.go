package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.zx2c4.com/wireguard/wgctrl"
)

var (
	wgPeersTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wireguard_peers_total",
		Help: "Total number of WireGuard peers",
	})

	wgAllowedIPsTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wireguard_allowed_ips_total",
		Help: "Total number of allowed IPs across all peers",
	})

	wgLastHandshake = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wireguard_last_handshake",
		Help: "Verifies the handshake status of a peer",
	})
)

// Fetch WireGuard metrics and update Prometheus metrics
func collectMetrics(client *wgctrl.Client, ifname string) {
	device, err := client.Device(ifname)
	if err != nil {
		log.Fatalf("Failed to fetch WireGuard devices: %v", err)
	}

	totalPeers := 0
	totalAllowedIPs := 0

	for _, peer := range device.Peers {
		totalPeers++
		totalAllowedIPs += len(peer.AllowedIPs)

		duration := time.Since(peer.LastHandshakeTime)

		if duration > 180*time.Second {
			//Issue with handshake
			wgLastHandshake.Set(0)
		} else {
			wgLastHandshake.Set(1)
		}
	}

	wgPeersTotal.Set(float64(totalPeers))
	wgAllowedIPsTotal.Set(float64(totalAllowedIPs))
}

func main() {
	var port, poll int
	var address, ifname string

	flag.StringVar(&ifname, "ifname", "cilium_wg0", "name of the wireguard interface")
	flag.StringVar(&address, "address", "0.0.0.0", "address for metrics endpoint")
	flag.IntVar(&poll, "poll", 60, "polling interval in seconds")
	flag.IntVar(&port, "port", 8080, "metrics port")
	flag.Parse()

	// Register Prometheus metrics
	prometheus.MustRegister(wgPeersTotal)
	prometheus.MustRegister(wgAllowedIPsTotal)
	prometheus.MustRegister(wgLastHandshake)

	// Start the metrics collection loop
	go func() {
		client, err := wgctrl.New()
		if err != nil {
			log.Fatalf("Failed to create WireGuard client: %v", err)
		}
		defer client.Close()

		for {

			fmt.Println("polling the wireguard interface")
			collectMetrics(client, ifname)
			time.Sleep(time.Duration(poll) * time.Second)
		}
	}()

	// Serve Prometheus metrics
	http.Handle("/metrics", promhttp.Handler())

	fmt.Printf("WireGuard Prometheus Exporter running on :%d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", address, port), nil))
}
