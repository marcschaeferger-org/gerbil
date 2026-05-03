// Package metrics provides the application-level metrics facade for Gerbil.
//
// Application code (main, relay, proxy) uses only the Record* functions in this
// package. The actual recording is delegated to the backend selected in
// internal/observability. Neither Prometheus nor OTel packages are imported here.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/fosrl/gerbil/internal/observability"
)

// Config is the metrics configuration type. It is an alias for
// observability.MetricsConfig so callers do not need to import observability.
type Config = observability.MetricsConfig

// PrometheusConfig is re-exported for convenience.
type PrometheusConfig = observability.PrometheusConfig

// OTelConfig is re-exported for convenience.
type OTelConfig = observability.OTelConfig

var (
	backend observability.Backend
	initMu  sync.Mutex

	// Interface and peer metrics
	wgInterfaceUp      observability.Int64Gauge
	wgPeersTotal       observability.UpDownCounter
	wgPeerConnected    observability.Int64Gauge
	wgHandshakesTotal  observability.Counter
	wgHandshakeLatency observability.Histogram
	wgPeerRTT          observability.Histogram
	wgBytesReceived    observability.Counter
	wgBytesTransmitted observability.Counter
	allowedIPsCount    observability.UpDownCounter
	keyRotationTotal   observability.Counter

	// System and proxy metrics
	netlinkEventsTotal     observability.Counter
	netlinkErrorsTotal     observability.Counter
	syncDuration           observability.Histogram
	workqueueDepth         observability.UpDownCounter
	kernelModuleLoads      observability.Counter
	firewallRulesApplied   observability.Counter
	activeSessions         observability.UpDownCounter
	activeProxyConnections observability.UpDownCounter
	proxyRouteLookups      observability.Counter
	proxyTLSHandshake      observability.Histogram
	proxyBytesTransmitted  observability.Counter

	// UDP Relay / Proxy Metrics
	udpPacketsTotal            observability.Counter
	udpPacketSizeBytes         observability.Histogram
	holePunchEventsTotal       observability.Counter
	proxyMappingActive         observability.UpDownCounter
	sessionRebuiltTotal        observability.Counter
	commPatternActive          observability.UpDownCounter
	proxyCleanupRemovedTotal   observability.Counter
	proxyConnectionErrorsTotal observability.Counter
	proxyInitialMappingsTotal  observability.Int64Gauge
	proxyMappingUpdatesTotal   observability.Counter
	proxyIdleCleanupDuration   observability.Histogram

	// SNI Proxy Metrics
	sniConnectionsTotal              observability.Counter
	sniConnectionDuration            observability.Histogram
	sniActiveConnections             observability.UpDownCounter
	sniRouteCacheHitsTotal           observability.Counter
	sniRouteAPIRequestsTotal         observability.Counter
	sniRouteAPILatency               observability.Histogram
	sniLocalOverrideTotal            observability.Counter
	sniTrustedProxyEventsTotal       observability.Counter
	sniProxyProtocolParseErrorsTotal observability.Counter
	sniDataBytesTotal                observability.Counter
	sniTunnelTerminationsTotal       observability.Counter

	// HTTP API & Peer Management Metrics
	httpRequestsTotal               observability.Counter
	httpRequestDuration             observability.Histogram
	peerOperationsTotal             observability.Counter
	proxyMappingUpdateRequestsTotal observability.Counter
	destinationsUpdateRequestsTotal observability.Counter

	// Remote Configuration, Reporting & Housekeeping
	remoteConfigFetchesTotal observability.Counter
	bandwidthReportsTotal    observability.Counter
	peerBandwidthBytesTotal  observability.Counter
	memorySpikeTotal         observability.Counter
	heapProfilesWrittenTotal observability.Counter

	// Operational metrics
	configReloadsTotal    observability.Counter
	restartTotal          observability.Counter
	authFailuresTotal     observability.Counter
	aclDeniedTotal        observability.Counter
	certificateExpiryDays observability.Float64Gauge
)

// DefaultConfig returns a default metrics configuration.
func DefaultConfig() Config {
	return observability.DefaultMetricsConfig()
}

// Initialize sets up the metrics system using the selected backend.
// It returns the /metrics HTTP handler (non-nil only for Prometheus backend).
func Initialize(cfg Config) (http.Handler, error) {
	initMu.Lock()
	defer initMu.Unlock()

	if backend != nil {
		return backend.HTTPHandler(), nil
	}

	b, err := observability.New(cfg)
	if err != nil {
		return nil, err
	}
	backend = b

	if err := createInstruments(); err != nil {
		backend = nil
		return nil, err
	}

	return backend.HTTPHandler(), nil
}

// Shutdown gracefully shuts down the metrics backend.
func Shutdown(ctx context.Context) error {
	initMu.Lock()
	b := backend
	backend = nil
	initMu.Unlock()

	if b != nil {
		return b.Shutdown(ctx)
	}
	return nil
}

func createInstruments() error {
	durationBuckets := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	sizeBuckets := []float64{512, 1024, 4096, 16384, 65536, 262144, 1048576}
	sniDurationBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}

	b := backend

	newCounter := func(name, desc string, labelNames ...string) (observability.Counter, error) {
		c, err := b.NewCounter(name, desc, labelNames...)
		if err != nil {
			return nil, fmt.Errorf("create counter %q: %w", name, err)
		}
		return c, nil
	}

	newUpDownCounter := func(name, desc string, labelNames ...string) (observability.UpDownCounter, error) {
		c, err := b.NewUpDownCounter(name, desc, labelNames...)
		if err != nil {
			return nil, fmt.Errorf("create updown counter %q: %w", name, err)
		}
		return c, nil
	}

	newInt64Gauge := func(name, desc string, labelNames ...string) (observability.Int64Gauge, error) {
		g, err := b.NewInt64Gauge(name, desc, labelNames...)
		if err != nil {
			return nil, fmt.Errorf("create int64 gauge %q: %w", name, err)
		}
		return g, nil
	}

	newFloat64Gauge := func(name, desc string, labelNames ...string) (observability.Float64Gauge, error) {
		g, err := b.NewFloat64Gauge(name, desc, labelNames...)
		if err != nil {
			return nil, fmt.Errorf("create float64 gauge %q: %w", name, err)
		}
		return g, nil
	}

	newHistogram := func(name, desc string, buckets []float64, labelNames ...string) (observability.Histogram, error) {
		h, err := b.NewHistogram(name, desc, buckets, labelNames...)
		if err != nil {
			return nil, fmt.Errorf("create histogram %q: %w", name, err)
		}
		return h, nil
	}

	var err error

	wgInterfaceUp, err = newInt64Gauge("gerbil_wg_interface_up",
		"Operational state of a WireGuard interface (1=up, 0=down)", "ifname", "instance")
	if err != nil {
		return err
	}
	wgPeersTotal, err = newUpDownCounter("gerbil_wg_peers_total",
		"Total number of configured peers per interface", "ifname")
	if err != nil {
		return err
	}
	wgPeerConnected, err = newInt64Gauge("gerbil_wg_peer_connected",
		"Whether a specific peer is connected (1=connected, 0=disconnected)", "ifname", "peer")
	if err != nil {
		return err
	}
	allowedIPsCount, err = newUpDownCounter("gerbil_allowed_ips_count",
		"Number of allowed IPs configured per peer", "ifname", "peer")
	if err != nil {
		return err
	}
	keyRotationTotal, err = newCounter("gerbil_key_rotation_total",
		"Key rotation events", "ifname", "reason")
	if err != nil {
		return err
	}
	wgHandshakesTotal, err = newCounter("gerbil_wg_handshakes_total",
		"Count of handshake attempts with their result status", "ifname", "peer", "result")
	if err != nil {
		return err
	}
	wgHandshakeLatency, err = newHistogram("gerbil_wg_handshake_latency_seconds",
		"Distribution of handshake latencies in seconds", durationBuckets, "ifname", "peer")
	if err != nil {
		return err
	}
	wgPeerRTT, err = newHistogram("gerbil_wg_peer_rtt_seconds",
		"Observed round-trip time to a peer in seconds", durationBuckets, "ifname", "peer")
	if err != nil {
		return err
	}
	wgBytesReceived, err = newCounter("gerbil_wg_bytes_received_total",
		"Number of bytes received from a peer", "ifname", "peer")
	if err != nil {
		return err
	}
	wgBytesTransmitted, err = newCounter("gerbil_wg_bytes_transmitted_total",
		"Number of bytes transmitted to a peer", "ifname", "peer")
	if err != nil {
		return err
	}
	netlinkEventsTotal, err = newCounter("gerbil_netlink_events_total",
		"Number of netlink events processed", "event_type")
	if err != nil {
		return err
	}
	netlinkErrorsTotal, err = newCounter("gerbil_netlink_errors_total",
		"Count of netlink or kernel errors", "component", "error_type")
	if err != nil {
		return err
	}
	syncDuration, err = newHistogram("gerbil_sync_duration_seconds",
		"Duration of reconciliation/sync loops in seconds", durationBuckets, "component")
	if err != nil {
		return err
	}
	workqueueDepth, err = newUpDownCounter("gerbil_workqueue_depth",
		"Current length of internal work queues", "queue")
	if err != nil {
		return err
	}
	kernelModuleLoads, err = newCounter("gerbil_kernel_module_loads_total",
		"Count of kernel module load attempts", "result")
	if err != nil {
		return err
	}
	firewallRulesApplied, err = newCounter("gerbil_firewall_rules_applied_total",
		"IPTables/NFT rules applied", "result", "chain")
	if err != nil {
		return err
	}
	activeSessions, err = newUpDownCounter("gerbil_active_sessions",
		"Number of active UDP relay sessions", "ifname")
	if err != nil {
		return err
	}
	activeProxyConnections, err = newUpDownCounter("gerbil_active_proxy_connections",
		"Active SNI proxy connections")
	if err != nil {
		return err
	}
	proxyRouteLookups, err = newCounter("gerbil_proxy_route_lookups_total",
		"Number of route lookups", "result")
	if err != nil {
		return err
	}
	proxyTLSHandshake, err = newHistogram("gerbil_proxy_tls_handshake_seconds",
		"TLS handshake duration for SNI proxy in seconds", durationBuckets)
	if err != nil {
		return err
	}
	proxyBytesTransmitted, err = newCounter("gerbil_proxy_bytes_transmitted_total",
		"Bytes sent/received by the SNI proxy", "direction")
	if err != nil {
		return err
	}
	configReloadsTotal, err = newCounter("gerbil_config_reloads_total",
		"Number of configuration reloads", "result")
	if err != nil {
		return err
	}
	restartTotal, err = newCounter("gerbil_restart_total",
		"Process restart count")
	if err != nil {
		return err
	}
	authFailuresTotal, err = newCounter("gerbil_auth_failures_total",
		"Count of authentication or peer validation failures", "peer", "reason")
	if err != nil {
		return err
	}
	aclDeniedTotal, err = newCounter("gerbil_acl_denied_total",
		"Access control denied events", "ifname", "peer", "policy")
	if err != nil {
		return err
	}
	certificateExpiryDays, err = newFloat64Gauge("gerbil_certificate_expiry_days",
		"Days until certificate expiry", "cert_name", "ifname")
	if err != nil {
		return err
	}
	udpPacketsTotal, err = newCounter("gerbil_udp_packets_total",
		"Count of UDP packets processed by relay workers", "ifname", "type", "direction")
	if err != nil {
		return err
	}
	udpPacketSizeBytes, err = newHistogram("gerbil_udp_packet_size_bytes",
		"Size distribution of packets forwarded through relay", sizeBuckets, "ifname", "type")
	if err != nil {
		return err
	}
	holePunchEventsTotal, err = newCounter("gerbil_hole_punch_events_total",
		"Count of hole punch messages processed", "ifname", "result")
	if err != nil {
		return err
	}
	proxyMappingActive, err = newUpDownCounter("gerbil_proxy_mapping_active",
		"Number of active proxy mappings", "ifname")
	if err != nil {
		return err
	}
	sessionRebuiltTotal, err = newCounter("gerbil_session_rebuilt_total",
		"Count of sessions rebuilt from communication patterns", "ifname")
	if err != nil {
		return err
	}
	commPatternActive, err = newUpDownCounter("gerbil_comm_pattern_active",
		"Number of active communication patterns", "ifname")
	if err != nil {
		return err
	}
	proxyCleanupRemovedTotal, err = newCounter("gerbil_proxy_cleanup_removed_total",
		"Count of items removed during cleanup routines", "ifname", "component")
	if err != nil {
		return err
	}
	proxyConnectionErrorsTotal, err = newCounter("gerbil_proxy_connection_errors_total",
		"Count of connection errors in proxy operations", "ifname", "error_type")
	if err != nil {
		return err
	}
	proxyInitialMappingsTotal, err = newInt64Gauge("gerbil_proxy_initial_mappings",
		"Number of initial proxy mappings loaded", "ifname")
	if err != nil {
		return err
	}
	proxyMappingUpdatesTotal, err = newCounter("gerbil_proxy_mapping_updates_total",
		"Count of proxy mapping updates", "ifname")
	if err != nil {
		return err
	}
	proxyIdleCleanupDuration, err = newHistogram("gerbil_proxy_idle_cleanup_duration_seconds",
		"Duration of cleanup cycles", durationBuckets, "ifname", "component")
	if err != nil {
		return err
	}
	sniConnectionsTotal, err = newCounter("gerbil_sni_connections_total",
		"Count of connections processed by SNI proxy", "result")
	if err != nil {
		return err
	}
	sniConnectionDuration, err = newHistogram("gerbil_sni_connection_duration_seconds",
		"Lifetime distribution of proxied TLS connections", sniDurationBuckets)
	if err != nil {
		return err
	}
	sniActiveConnections, err = newUpDownCounter("gerbil_sni_active_connections",
		"Number of active SNI tunnels")
	if err != nil {
		return err
	}
	sniRouteCacheHitsTotal, err = newCounter("gerbil_sni_route_cache_hits_total",
		"Count of route cache hits and misses", "result")
	if err != nil {
		return err
	}
	sniRouteAPIRequestsTotal, err = newCounter("gerbil_sni_route_api_requests_total",
		"Count of route API requests", "result")
	if err != nil {
		return err
	}
	sniRouteAPILatency, err = newHistogram("gerbil_sni_route_api_latency_seconds",
		"Distribution of route API call latencies", durationBuckets)
	if err != nil {
		return err
	}
	sniLocalOverrideTotal, err = newCounter("gerbil_sni_local_override_total",
		"Count of routes using local overrides", "hit")
	if err != nil {
		return err
	}
	sniTrustedProxyEventsTotal, err = newCounter("gerbil_sni_trusted_proxy_events_total",
		"Count of PROXY protocol events", "event")
	if err != nil {
		return err
	}
	sniProxyProtocolParseErrorsTotal, err = newCounter("gerbil_sni_proxy_protocol_parse_errors_total",
		"Count of PROXY protocol parse failures")
	if err != nil {
		return err
	}
	sniDataBytesTotal, err = newCounter("gerbil_sni_data_bytes_total",
		"Count of bytes proxied through SNI tunnels", "direction")
	if err != nil {
		return err
	}
	sniTunnelTerminationsTotal, err = newCounter("gerbil_sni_tunnel_terminations_total",
		"Count of tunnel terminations by reason", "reason")
	if err != nil {
		return err
	}
	httpRequestsTotal, err = newCounter("gerbil_http_requests_total",
		"Count of HTTP requests to management API", "endpoint", "method", "status_code")
	if err != nil {
		return err
	}
	httpRequestDuration, err = newHistogram("gerbil_http_request_duration_seconds",
		"Distribution of HTTP request handling time", durationBuckets, "endpoint", "method")
	if err != nil {
		return err
	}
	peerOperationsTotal, err = newCounter("gerbil_peer_operations_total",
		"Count of peer lifecycle operations", "operation", "result")
	if err != nil {
		return err
	}
	proxyMappingUpdateRequestsTotal, err = newCounter("gerbil_proxy_mapping_update_requests_total",
		"Count of proxy mapping update API calls", "result")
	if err != nil {
		return err
	}
	destinationsUpdateRequestsTotal, err = newCounter("gerbil_destinations_update_requests_total",
		"Count of destinations update API calls", "result")
	if err != nil {
		return err
	}
	remoteConfigFetchesTotal, err = newCounter("gerbil_remote_config_fetches_total",
		"Count of remote configuration fetch attempts", "result")
	if err != nil {
		return err
	}
	bandwidthReportsTotal, err = newCounter("gerbil_bandwidth_reports_total",
		"Count of bandwidth report transmissions", "result")
	if err != nil {
		return err
	}
	peerBandwidthBytesTotal, err = newCounter("gerbil_peer_bandwidth_bytes_total",
		"Bytes per peer tracked by bandwidth calculation", "peer", "direction")
	if err != nil {
		return err
	}
	memorySpikeTotal, err = newCounter("gerbil_memory_spike_total",
		"Count of memory spikes detected", "severity")
	if err != nil {
		return err
	}
	heapProfilesWrittenTotal, err = newCounter("gerbil_heap_profiles_written_total",
		"Count of heap profile files generated")
	if err != nil {
		return err
	}

	return nil
}

func RecordInterfaceUp(ifname, instance string, up bool) {
	if wgInterfaceUp == nil {
		return
	}
	value := int64(0)
	if up {
		value = 1
	}
	wgInterfaceUp.Record(context.Background(), value, observability.Labels{"ifname": ifname, "instance": instance})
}

func RecordPeersTotal(ifname string, delta int64) {
	if wgPeersTotal == nil {
		return
	}
	wgPeersTotal.Add(context.Background(), delta, observability.Labels{"ifname": ifname})
}

func RecordPeerConnected(ifname, peer string, connected bool) {
	if wgPeerConnected == nil {
		return
	}
	value := int64(0)
	if connected {
		value = 1
	}
	wgPeerConnected.Record(context.Background(), value, observability.Labels{"ifname": ifname, "peer": peer})
}

func RecordHandshake(ifname, peer, result string) {
	if wgHandshakesTotal == nil {
		return
	}
	wgHandshakesTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname, "peer": peer, "result": result})
}

func RecordHandshakeLatency(ifname, peer string, seconds float64) {
	if wgHandshakeLatency == nil {
		return
	}
	wgHandshakeLatency.Record(context.Background(), seconds, observability.Labels{"ifname": ifname, "peer": peer})
}

func RecordPeerRTT(ifname, peer string, seconds float64) {
	if wgPeerRTT == nil {
		return
	}
	wgPeerRTT.Record(context.Background(), seconds, observability.Labels{"ifname": ifname, "peer": peer})
}

func RecordBytesReceived(ifname, peer string, bytes int64) {
	if wgBytesReceived == nil {
		return
	}
	wgBytesReceived.Add(context.Background(), bytes, observability.Labels{"ifname": ifname, "peer": peer})
}

func RecordBytesTransmitted(ifname, peer string, bytes int64) {
	if wgBytesTransmitted == nil {
		return
	}
	wgBytesTransmitted.Add(context.Background(), bytes, observability.Labels{"ifname": ifname, "peer": peer})
}

func RecordAllowedIPsCount(ifname, peer string, delta int64) {
	if allowedIPsCount == nil {
		return
	}
	allowedIPsCount.Add(context.Background(), delta, observability.Labels{"ifname": ifname, "peer": peer})
}

func RecordKeyRotation(ifname, reason string) {
	if keyRotationTotal == nil {
		return
	}
	keyRotationTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname, "reason": reason})
}

func RecordNetlinkEvent(eventType string) {
	if netlinkEventsTotal == nil {
		return
	}
	netlinkEventsTotal.Add(context.Background(), 1, observability.Labels{"event_type": eventType})
}

func RecordNetlinkError(component, errorType string) {
	if netlinkErrorsTotal == nil {
		return
	}
	netlinkErrorsTotal.Add(context.Background(), 1, observability.Labels{"component": component, "error_type": errorType})
}

func RecordSyncDuration(component string, seconds float64) {
	if syncDuration == nil {
		return
	}
	syncDuration.Record(context.Background(), seconds, observability.Labels{"component": component})
}

func RecordWorkqueueDepth(queue string, delta int64) {
	if workqueueDepth == nil {
		return
	}
	workqueueDepth.Add(context.Background(), delta, observability.Labels{"queue": queue})
}

func RecordKernelModuleLoad(result string) {
	if kernelModuleLoads == nil {
		return
	}
	kernelModuleLoads.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordFirewallRuleApplied(result, chain string) {
	if firewallRulesApplied == nil {
		return
	}
	firewallRulesApplied.Add(context.Background(), 1, observability.Labels{"result": result, "chain": chain})
}

func RecordActiveSession(ifname string, delta int64) {
	if activeSessions == nil {
		return
	}
	activeSessions.Add(context.Background(), delta, observability.Labels{"ifname": ifname})
}

func RecordActiveProxyConnection(delta int64) {
	if activeProxyConnections == nil {
		return
	}
	activeProxyConnections.Add(context.Background(), delta, nil)
}

func RecordProxyRouteLookup(result string) {
	if proxyRouteLookups == nil {
		return
	}
	proxyRouteLookups.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordProxyTLSHandshake(seconds float64) {
	if proxyTLSHandshake == nil {
		return
	}
	proxyTLSHandshake.Record(context.Background(), seconds, nil)
}

func RecordProxyBytesTransmitted(direction string, bytes int64) {
	if proxyBytesTransmitted == nil {
		return
	}
	proxyBytesTransmitted.Add(context.Background(), bytes, observability.Labels{"direction": direction})
}

func RecordConfigReload(result string) {
	if configReloadsTotal == nil {
		return
	}
	configReloadsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordRestart() {
	if restartTotal == nil {
		return
	}
	restartTotal.Add(context.Background(), 1, nil)
}

func RecordAuthFailure(peer, reason string) {
	if authFailuresTotal == nil {
		return
	}
	authFailuresTotal.Add(context.Background(), 1, observability.Labels{"peer": peer, "reason": reason})
}

func RecordACLDenied(ifname, peer, policy string) {
	if aclDeniedTotal == nil {
		return
	}
	aclDeniedTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname, "peer": peer, "policy": policy})
}

func RecordCertificateExpiry(certName, ifname string, days float64) {
	if certificateExpiryDays == nil {
		return
	}
	certificateExpiryDays.Record(context.Background(), days, observability.Labels{"cert_name": certName, "ifname": ifname})
}

func RecordUDPPacket(ifname, packetType, direction string) {
	if udpPacketsTotal == nil {
		return
	}
	udpPacketsTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname, "type": packetType, "direction": direction})
}

func RecordUDPPacketSize(ifname, packetType string, bytes float64) {
	if udpPacketSizeBytes == nil {
		return
	}
	udpPacketSizeBytes.Record(context.Background(), bytes, observability.Labels{"ifname": ifname, "type": packetType})
}

func RecordHolePunchEvent(ifname, result string) {
	if holePunchEventsTotal == nil {
		return
	}
	holePunchEventsTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname, "result": result})
}

func RecordProxyMapping(ifname string, delta int64) {
	if proxyMappingActive == nil {
		return
	}
	proxyMappingActive.Add(context.Background(), delta, observability.Labels{"ifname": ifname})
}

func RecordSession(ifname string, delta int64) {
	if activeSessions == nil {
		return
	}
	activeSessions.Add(context.Background(), delta, observability.Labels{"ifname": ifname})
}

func RecordSessionRebuilt(ifname string) {
	if sessionRebuiltTotal == nil {
		return
	}
	sessionRebuiltTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname})
}

func RecordCommPattern(ifname string, delta int64) {
	if commPatternActive == nil {
		return
	}
	commPatternActive.Add(context.Background(), delta, observability.Labels{"ifname": ifname})
}

func RecordProxyCleanupRemoved(ifname, component string, count int64) {
	if proxyCleanupRemovedTotal == nil {
		return
	}
	proxyCleanupRemovedTotal.Add(context.Background(), count, observability.Labels{"ifname": ifname, "component": component})
}

func RecordProxyConnectionError(ifname, errorType string) {
	if proxyConnectionErrorsTotal == nil {
		return
	}
	proxyConnectionErrorsTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname, "error_type": errorType})
}

func RecordProxyInitialMappings(ifname string, count int64) {
	if proxyInitialMappingsTotal == nil {
		return
	}
	proxyInitialMappingsTotal.Record(context.Background(), count, observability.Labels{"ifname": ifname})
}

func RecordProxyMappingUpdate(ifname string) {
	if proxyMappingUpdatesTotal == nil {
		return
	}
	proxyMappingUpdatesTotal.Add(context.Background(), 1, observability.Labels{"ifname": ifname})
}

func RecordProxyIdleCleanupDuration(ifname, component string, seconds float64) {
	if proxyIdleCleanupDuration == nil {
		return
	}
	proxyIdleCleanupDuration.Record(context.Background(), seconds, observability.Labels{"ifname": ifname, "component": component})
}

func RecordSNIConnection(result string) {
	if sniConnectionsTotal == nil {
		return
	}
	sniConnectionsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordSNIConnectionDuration(seconds float64) {
	if sniConnectionDuration == nil {
		return
	}
	sniConnectionDuration.Record(context.Background(), seconds, nil)
}

func RecordSNIActiveConnection(delta int64) {
	if sniActiveConnections == nil {
		return
	}
	sniActiveConnections.Add(context.Background(), delta, nil)
}

func RecordSNIRouteCacheHit(result string) {
	if sniRouteCacheHitsTotal == nil {
		return
	}
	sniRouteCacheHitsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordSNIRouteAPIRequest(result string) {
	if sniRouteAPIRequestsTotal == nil {
		return
	}
	sniRouteAPIRequestsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordSNIRouteAPILatency(seconds float64) {
	if sniRouteAPILatency == nil {
		return
	}
	sniRouteAPILatency.Record(context.Background(), seconds, nil)
}

func RecordSNILocalOverride(hit string) {
	if sniLocalOverrideTotal == nil {
		return
	}
	sniLocalOverrideTotal.Add(context.Background(), 1, observability.Labels{"hit": hit})
}

func RecordSNITrustedProxyEvent(event string) {
	if sniTrustedProxyEventsTotal == nil {
		return
	}
	sniTrustedProxyEventsTotal.Add(context.Background(), 1, observability.Labels{"event": event})
}

func RecordSNIProxyProtocolParseError() {
	if sniProxyProtocolParseErrorsTotal == nil {
		return
	}
	sniProxyProtocolParseErrorsTotal.Add(context.Background(), 1, nil)
}

func RecordSNIDataBytes(direction string, bytes int64) {
	if sniDataBytesTotal == nil {
		return
	}
	sniDataBytesTotal.Add(context.Background(), bytes, observability.Labels{"direction": direction})
}

func RecordSNITunnelTermination(reason string) {
	if sniTunnelTerminationsTotal == nil {
		return
	}
	sniTunnelTerminationsTotal.Add(context.Background(), 1, observability.Labels{"reason": reason})
}

func RecordHTTPRequest(endpoint, method, statusCode string) {
	if httpRequestsTotal == nil {
		return
	}
	httpRequestsTotal.Add(context.Background(), 1, observability.Labels{"endpoint": endpoint, "method": method, "status_code": statusCode})
}

func RecordHTTPRequestDuration(endpoint, method string, seconds float64) {
	if httpRequestDuration == nil {
		return
	}
	httpRequestDuration.Record(context.Background(), seconds, observability.Labels{"endpoint": endpoint, "method": method})
}

func RecordPeerOperation(operation, result string) {
	if peerOperationsTotal == nil {
		return
	}
	peerOperationsTotal.Add(context.Background(), 1, observability.Labels{"operation": operation, "result": result})
}

func RecordProxyMappingUpdateRequest(result string) {
	if proxyMappingUpdateRequestsTotal == nil {
		return
	}
	proxyMappingUpdateRequestsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordDestinationsUpdateRequest(result string) {
	if destinationsUpdateRequestsTotal == nil {
		return
	}
	destinationsUpdateRequestsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordRemoteConfigFetch(result string) {
	if remoteConfigFetchesTotal == nil {
		return
	}
	remoteConfigFetchesTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordBandwidthReport(result string) {
	if bandwidthReportsTotal == nil {
		return
	}
	bandwidthReportsTotal.Add(context.Background(), 1, observability.Labels{"result": result})
}

func RecordPeerBandwidthBytes(peer, direction string, bytes int64) {
	if peerBandwidthBytesTotal == nil {
		return
	}
	peerBandwidthBytesTotal.Add(context.Background(), bytes, observability.Labels{"peer": peer, "direction": direction})
}

func RecordMemorySpike(severity string) {
	if memorySpikeTotal == nil {
		return
	}
	memorySpikeTotal.Add(context.Background(), 1, observability.Labels{"severity": severity})
}

func RecordHeapProfileWritten() {
	if heapProfilesWrittenTotal == nil {
		return
	}
	heapProfilesWrittenTotal.Add(context.Background(), 1, nil)
}
