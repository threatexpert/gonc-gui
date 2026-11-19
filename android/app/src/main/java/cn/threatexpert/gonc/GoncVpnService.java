package cn.threatexpert.gonc;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.system.OsConstants;

import androidx.core.app.NotificationCompat;

import java.io.IOException;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.security.SecureRandom;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import mobilegonc.Callback;
import mobilegonc.Mobilegonc;

public final class GoncVpnService extends VpnService {
    static final String ACTION_START = "cn.threatexpert.gonc.START_VPN";
    static final String ACTION_STOP = "cn.threatexpert.gonc.STOP_VPN";
    static final String EXTRA_PASSWORD = "password";
    static final String EXTRA_USE_UDP = "use_udp";
    static final String EXTRA_P2P_WITH_LAN = "p2p_with_lan";
    static final String EXTRA_DNS_SERVERS = "dns_servers";
    static final String EXTRA_ROUTE_CIDRS = "route_cidrs";
    static final String EXTRA_LINK_CONFIG = "link_config";
    static final String EXTRA_MTU = "mtu";
    static final String EXTRA_EXTRA_ARGS = "extra_args";
    static final String EXTRA_TUNNEL_ONLY = "tunnel_only";
    static final String EXTRA_DISCONNECT_PROTECTION = "disconnect_protection";
    static final String EXTRA_RECONNECT_DNS = "reconnect_dns";
    static final String EXTRA_PROTECTION_CONFIRMED = "protection_confirmed";

    private static final int NOTIFICATION_ID = 2001;
    private static final String CHANNEL_ID = "gonc_vpn";
    private static final int DEFAULT_MTU = 1400;
    private static final int AUTO_PORT_START = 16000;
    private static final int AUTO_PORT_COUNT = 1000;
    private static final SecureRandom PORT_RANDOM = new SecureRandom();

    private final Object lock = new Object();
    private final Object engineLock = new Object();
    private Thread vpnWorker;
    private ParcelFileDescriptor vpnInterface;
    private mobilegonc.Session goncSession;
    private Runnable releaseMulticast;
    private boolean tun2socksRunning;
    private boolean stopRequested;
    private boolean stopWorkerRunning;
    private volatile String lastGoncMessage = "";
    private volatile boolean disconnectProtection;
    private volatile ProtectedTunnelAttempt protectedAttempt;

    @Override
    public void onCreate() {
        super.onCreate();
        GoncCrashReporter.install(this);
        GoncCrashReporter.stage(this, "vpn service created");
        createNotificationChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();
        if (ACTION_STOP.equals(action)) {
            stopVpn();
            return START_NOT_STICKY;
        }
        if (ACTION_START.equals(action)) {
            try {
                GoncCrashReporter.stage(this, "startForeground");
                startForeground(NOTIFICATION_ID, notification(connectingText()));
            } catch (RuntimeException error) {
                GoncCrashReporter.recordNonFatal(this, "Cannot start foreground service", error);
                GoncVpnState.setError(error.getMessage() == null ? error.toString() : error.getMessage());
                log("error", "Cannot start VPN foreground service: " + (error.getMessage() == null ? error.toString() : error.getMessage()));
                stopSelf();
                return START_NOT_STICKY;
            }
            String password = intent.getStringExtra(EXTRA_PASSWORD);
            boolean useUdp = intent.getBooleanExtra(EXTRA_USE_UDP, false);
            boolean p2pWithLan = intent.getBooleanExtra(EXTRA_P2P_WITH_LAN, false);
            String dnsServers = intent.getStringExtra(EXTRA_DNS_SERVERS);
            String routeCidrs = intent.getStringExtra(EXTRA_ROUTE_CIDRS);
            String linkConfig = intent.getStringExtra(EXTRA_LINK_CONFIG);
            int mtu = intent.getIntExtra(EXTRA_MTU, DEFAULT_MTU);
            String extraArgs = intent.getStringExtra(EXTRA_EXTRA_ARGS);
            boolean tunnelOnly = intent.getBooleanExtra(EXTRA_TUNNEL_ONLY, false);
            startVpn(password, useUdp, p2pWithLan, dnsServers, routeCidrs, linkConfig, mtu, extraArgs, tunnelOnly,
                    intent.getBooleanExtra(EXTRA_DISCONNECT_PROTECTION, false),
                    intent.getStringExtra(EXTRA_RECONNECT_DNS), intent.getBooleanExtra(EXTRA_PROTECTION_CONFIRMED, false));
            return START_STICKY;
        }
        return START_NOT_STICKY;
    }

    private void startVpn(String password, boolean useUdp, boolean p2pWithLan, String dnsServers, String routeCidrs, String linkConfig, int mtu, String extraArgs, boolean tunnelOnly, boolean protection, String reconnectDns, boolean confirmed) {
        boolean enableIpv6 = VpnRoutePolicy.hasIpv6(routeCidrs);
        synchronized (lock) {
            if (goncSession != null || vpnInterface != null || tun2socksRunning || vpnWorker != null || stopWorkerRunning) {
                log("warn", "VPN is already running");
                return;
            }
            stopRequested = false;
            disconnectProtection = protection && !tunnelOnly;
        }
        lastGoncMessage = "";
        GoncVpnState.setStatus(GoncVpnState.CONNECTING);
        GoncVpnState.setPeerIpv6(tunnelOnly ? "-" : (enableIpv6 ? "waiting" : "disabled"));
        log("info", tunnelOnly ? "Starting SOCKS5 tunnel (tunnel only)" : "Starting VPN tunnel");

        Thread worker = new Thread(() -> {
            try {
                if (!tunnelOnly) {
                    VpnRoutePolicy.routes(routeCidrs); // Reject invalid rules before starting anything.
                    VpnRoutePolicy.dns(dnsServers, true);
                }
                if (isStopRequested()) {
                    return;
                }
                GoncCrashReporter.stage(this, "resolve link config");
                String effectiveLink = resolveLinkConfig(linkConfig);
                int effectiveMtu = normalizeMtu(mtu);
                log("info", "Using link config " + effectiveLink);

                if (isStopRequested()) {
                    return;
                }
                if (disconnectProtection) {
                    VpnProtection.Options options = VpnProtection.options(extraArgs, reconnectDns);
                    if ((options.hadDns || options.hasLocal) && !confirmed) {
                        throw new IllegalArgumentException("Confirm advanced -dns/-local arguments before enabling VPN protection");
                    }
                    synchronized (lock) {
                        if (stopRequested) return;
                        releaseMulticast = VpnDiscovery.acquireMulticast(this, p2pWithLan, message -> log("warn", message));
                    }
                    runProtectedTunnel(password, useUdp, effectiveLink, VpnDiscovery.extraArgs(options.arguments, p2pWithLan),
                            enableIpv6, dnsServers, routeCidrs, effectiveMtu);
                    return;
                }
                GoncCrashReporter.stage(this, "start gonc tunnel");
                TunnelReadySignal tunnelReady = new TunnelReadySignal();
                synchronized (lock) {
                    if (stopRequested) return;
                    releaseMulticast = VpnDiscovery.acquireMulticast(this, p2pWithLan, message -> log("warn", message));
                }
                mobilegonc.Session session = Mobilegonc.startP2PTunnel(password, useUdp, effectiveLink,
                        VpnDiscovery.extraArgs(extraArgs, p2pWithLan), vpnCallback(tunnelReady, tunnelOnly));
                boolean stopSessionNow = false;
                synchronized (lock) {
                    if (stopRequested) {
                        stopSessionNow = true;
                    } else {
                        goncSession = session;
                    }
                }
                if (stopSessionNow) {
                    GoncCrashReporter.stage(this, "stop gonc tunnel before tun2socks");
                    try {
                        session.stop();
                    } catch (Throwable error) {
                        GoncCrashReporter.recordNonFatal(this, "Error stopping canceled gonc", error);
                        log("error", "Error stopping canceled gonc: " + (error.getMessage() == null ? error.toString() : error.getMessage()));
                    }
                    return;
                }

                if (tunnelOnly) {
                    // No system VPN interface / tun2socks: the SOCKS5 tunnel is the whole
                    // job. SOCKS5 readiness is the point where the tunnel is usable.
                    GoncVpnState.setPeerIpv6("-");
                    return;
                }

                GoncCrashReporter.stage(this, "wait for SOCKS5 endpoint before VPN interface");
                String readySocks5Endpoint = waitForSocks5Ready(tunnelReady);
                if (readySocks5Endpoint.isEmpty()) {
                    return;
                }

                long probeRevision = tunnelReady.revision();
                boolean effectiveIpv6 = resolveEffectiveIpv6(enableIpv6, readySocks5Endpoint, tunnelReady, probeRevision);
                if (isStopRequested()) {
                    return;
                }

                List<String> selectedDns = selectedDns(dnsServers, effectiveIpv6);
                installInterface(enableIpv6, selectedDns, routeCidrs, effectiveMtu, readySocks5Endpoint,
                        VpnRoutePolicy.rejectIpv6(routeCidrs, effectiveIpv6));
                if (enableIpv6 && !isStopRequested()) {
                    monitorIpv6Reconnects(tunnelReady, probeRevision, selectedDns, dnsServers, routeCidrs, effectiveMtu);
                }
            } catch (Throwable error) {
                if (isStopRequested()) return;
                GoncCrashReporter.recordNonFatal(this, "VPN start failed", error);
                log("error", error.getMessage() == null ? error.toString() : error.getMessage());
                GoncVpnState.setError(error.getMessage() == null ? error.toString() : error.getMessage());
                stopVpn();
            } finally {
                synchronized (lock) { vpnWorker = null; lock.notifyAll(); }
            }
        }, "gonc-vpn-start");
        synchronized (lock) { vpnWorker = worker; }
        worker.start();
    }

    private boolean hasCapture() {
        synchronized (lock) { return vpnInterface != null; }
    }

    private void protectionWaiting(String reason) {
        if (isStopRequested()) return;
        GoncVpnState.setProtectionWaiting(hasCapture(), reason);
        updateNotification(notificationStatusText());
    }

    // A live attempt waits on callbacks, with no timer/network inspection. The
    // retry delay exists only after a reported failure or completed session.
    private void runProtectedTunnel(String password, boolean udp, String link, String args,
                                    boolean captureIpv6, String dns, String routes, int mtu) throws Exception {
        String activeEndpoint = "";
        boolean policyChosen = false, rejectIpv6 = false;
        List<String> activeDns = null;
        while (!isStopRequested()) {
            ProtectedTunnelAttempt attempt = new ProtectedTunnelAttempt();
            protectedAttempt = attempt;
            TunnelReadySignal ready = new TunnelReadySignal();
            mobilegonc.Session session = null;
            try {
                if (isStopRequested()) return;
                session = Mobilegonc.startP2PTunnel(password, udp, link, args, protectedCallback(attempt, ready));
                synchronized (lock) { goncSession = session; }
                long seen = 0;
                while (!isStopRequested() && !attempt.lost()) {
                    attempt.awaitChange(seen);
                    if (isStopRequested() || attempt.lost() || !attempt.active()) break;
                    seen = attempt.revision();
                    String endpoint = attempt.endpoint();
                    if (endpoint.isEmpty()) continue;
                    if (!policyChosen) {
                        boolean available = resolveEffectiveIpv6(captureIpv6, endpoint, ready, ready.revision());
                        if (isStopRequested() || attempt.lost()) continue;
                        activeDns = selectedDns(dns, available);
                        rejectIpv6 = VpnRoutePolicy.rejectIpv6(routes, available);
                        policyChosen = true;
                    }
                    synchronized (engineLock) {
                        if (isStopRequested() || attempt.lost()) continue;
                        if (!hasCapture()) {
                            installInterface(captureIpv6, activeDns, routes, mtu, endpoint, rejectIpv6);
                        } else if (!tun2socksRunning) {
                            restartEngine(endpoint, mtu, rejectIpv6);
                        } else if (!endpoint.equals(activeEndpoint)) {
                            Mobilegonc.updateTun2SocksProxy(endpoint);
                            log("info", "Updated SOCKS5 endpoint without restarting TUN");
                        }
                        activeEndpoint = endpoint;
                        // Serializes with loss callbacks, avoiding a late Connected
                        // after a loss received during interface/engine setup.
                        synchronized (attempt) {
                            if (attempt.active() && !attempt.lost() && !isStopRequested()) markConnected();
                        }
                    }
                }
            } catch (Exception error) {
                if (isStopRequested()) return;
                if (!hasCapture()) throw error;
                log("error", "Protected reconnect failed: " + error.getMessage());
                protectionWaiting(error.getMessage());
            } finally {
                attempt.retire();
                if (session != null) {
                    session.stop();
                    // Wait for old Go globals/listeners to be released before a new
                    // session starts. Explicit Stop can hand cleanup to stopVpn.
                    boolean done;
                    do { done = session.awaitDone(1000); }
                    while (!done && !isStopRequested());
                    if (done) {
                        synchronized (lock) { if (goncSession == session) goncSession = null; }
                    }
                }
                if (protectedAttempt == attempt) protectedAttempt = null;
            }
            if (!isStopRequested()) {
                protectionWaiting(lastGoncMessage);
                synchronized (lock) { if (!stopRequested) lock.wait(2000); }
            }
        }
    }

    private Callback protectedCallback(ProtectedTunnelAttempt attempt, TunnelReadySignal ready) {
        return new Callback() {
            @Override public void event(String level, String message) {
                synchronized (attempt) {
                    if (!attempt.active() || isStopRequested()) return;
                    lastGoncMessage = message == null ? "" : message;
                    log(level, lastGoncMessage);
                }
            }
            @Override public void p2PReport(String topic, String side, String status, String network, String mode, String peer, long at, long pid) {
                synchronized (attempt) {
                    if (isStopRequested() || !attempt.report(topic, status)) return;
                    GoncVpnState.setP2PReport(status, network, mode, peer);
                    if (attempt.lost()) protectionWaiting(status);
                    else updateNotification(notificationStatusText());
                }
            }
            @Override public void ready(String endpoint) {
                synchronized (attempt) {
                    if (isStopRequested() || !attempt.ready(endpoint)) return;
                    GoncVpnState.setEndpoint(endpoint);
                    ready.markReady(endpoint);
                }
            }
            @Override public void traffic(String side, long in, long out, double ib, double ob, long elapsed, long count, boolean last) {
                synchronized (attempt) {
                    if (attempt.active() && !isStopRequested()) GoncVpnState.setTraffic(in, out, ib, ob);
                }
            }
            @Override public void stopped(long code) { lost("gonc session stopped: " + code); }
            @Override public void error(String message) { lost(message); }
            private void lost(String reason) {
                synchronized (attempt) {
                    if (!attempt.active() || isStopRequested()) return;
                    lastGoncMessage = reason == null ? "" : reason;
                    log("warn", lastGoncMessage);
                    attempt.fail();
                    protectionWaiting(lastGoncMessage);
                }
            }
        };
    }

    // The Java-owned descriptor remains open for the entire VPN lifetime. The
    // engine receives a detached duplicate, preserving the existing fdsan-safe
    // transfer convention and preventing a route gap during DNS reconfiguration.
    private void installInterface(boolean captureIpv6, List<String> dns, String routes, int mtu, String endpoint, boolean rejectIpv6) throws Exception {
        synchronized (engineLock) {
            if (isStopRequested()) return;
            GoncVpnState.setSystemVpnReady(false);
            ParcelFileDescriptor next = establishInterface(captureIpv6, dns, routes, mtu);
            if (next == null) throw new IllegalStateException("Could not establish VPN interface");
            ParcelFileDescriptor previous;
            synchronized (lock) {
                previous = vpnInterface;
                vpnInterface = next;
            }
            try {
                restartEngine(endpoint, mtu, rejectIpv6);
                log("info", "All configured routes installed; DNS: " + String.join(", ", dns));
            } finally {
                closeParcelQuietly(previous);
            }
        }
    }

    // Reuse the Java-owned TUN when DNS is unchanged. Rebuilding the engine on
    // every new tunnel generation applies rejection changes and clears old SYN
    // failure records without removing the VPN routes.
    private void restartEngine(String endpoint, int mtu, boolean rejectIpv6) throws Exception {
        synchronized (engineLock) {
            if (isStopRequested()) return;
            GoncVpnState.setSystemVpnReady(false);
            ParcelFileDescriptor tun;
            boolean wasRunning;
            synchronized (lock) {
                tun = vpnInterface;
                wasRunning = tun2socksRunning;
            }
            if (tun == null) throw new IllegalStateException("VPN interface is not available");
            if (wasRunning) {
                if (disconnectProtection) Mobilegonc.stopTun2SocksWithError();
                else Mobilegonc.stopTun2Socks();
                synchronized (lock) { tun2socksRunning = false; }
            }
            int fd = -1;
            try {
                if (isStopRequested()) return;
                fd = ParcelFileDescriptor.dup(tun.getFileDescriptor()).detachFd();
                Mobilegonc.startTun2SocksWithPolicy(fd, endpoint, "tun0", mtu, "warn", true, rejectIpv6);
                synchronized (lock) { tun2socksRunning = true; }
                log("info", "tun2socks started; deferred TCP handshake enabled; local IPv6 rejection=" + rejectIpv6);
                if (!isStopRequested() && !disconnectProtection) markConnected();
            } finally {
                closeDetachedFd(fd);
            }
        }
    }

    private List<String> selectedDns(String configured, boolean ipv6Available) {
        List<String> selected = VpnRoutePolicy.dns(configured, ipv6Available);
        if (!ipv6Available && VpnRoutePolicy.dns(configured, true).stream().allMatch(value -> value.contains(":"))) {
            log("warn", "No usable configured DNS after IPv6 filtering; using 8.8.8.8 through the VPN");
        }
        return selected;
    }

    private void monitorIpv6Reconnects(TunnelReadySignal ready, long revision, List<String> activeDns,
                             String configuredDns, String routes, int mtu) throws Exception {
        boolean waitingForReconnect = false;
        while (!isStopRequested()) {
            Thread.sleep(250);
            if (!"connected".equals(normalizeP2PStatus(GoncVpnState.p2pStatus()))) {
                if (!waitingForReconnect) GoncVpnState.setPeerIpv6("waiting");
                waitingForReconnect = true;
                continue;
            }
            long currentRevision = ready.revision();
            // Only a new SOCKS5 Ready generation triggers another probe.
            if (currentRevision == revision) continue;
            waitingForReconnect = false;
            revision = currentRevision;
            String endpoint = ready.awaitReadyEndpoint(0);
            boolean nextAvailable = resolveEffectiveIpv6(true, endpoint, ready, revision);
            if (isStopRequested()) return;
            if (revision != ready.revision() || !"connected".equals(normalizeP2PStatus(GoncVpnState.p2pStatus()))) continue;
            List<String> nextDns = selectedDns(configuredDns, nextAvailable);
            boolean rejectIpv6 = VpnRoutePolicy.rejectIpv6(routes, nextAvailable);
            if (!nextDns.equals(activeDns)) {
                installInterface(true, nextDns, routes, mtu, endpoint, rejectIpv6);
                activeDns = nextDns;
            } else {
                restartEngine(endpoint, mtu, rejectIpv6);
            }
        }
    }

    private String waitForSocks5Ready(TunnelReadySignal tunnelReady) throws InterruptedException {
        log("info", "Waiting for SOCKS5 endpoint before starting system VPN");
        while (!isStopRequested()) {
            String endpoint = tunnelReady.awaitReadyEndpoint(250);
            if (!endpoint.isEmpty()) {
                log("info", "SOCKS5 endpoint ready; starting remote IPv6 check and system VPN");
                return endpoint;
            }
        }
        return "";
    }

    private boolean resolveEffectiveIpv6(boolean requestedIpv6, String socks5Endpoint, TunnelReadySignal ready, long revision) {
        if (isStopRequested()) return false;
        if (!requestedIpv6) {
            GoncVpnState.setPeerIpv6("disabled");
            return false;
        }
        GoncVpnState.setPeerIpv6("checking");
        log("info", "Checking remote IPv6 availability");
        RemoteIpv6Probe.Result result = RemoteIpv6Probe.check(socks5Endpoint, RemoteIpv6Probe.DEFAULT_TIMEOUT_MS);
        if (isStopRequested()) return false;
        if (revision != ready.revision() || !"connected".equals(normalizeP2PStatus(GoncVpnState.p2pStatus()))) {
            GoncVpnState.setPeerIpv6("waiting");
            return false;
        }
        if (result.available) {
            GoncVpnState.setPeerIpv6("available");
            log("info", "Remote IPv6 is available via " + result.detail + "; configured IPv6 routes remain captured");
            return true;
        }
        GoncVpnState.setPeerIpv6("unavailable");
        log("warn", "Remote IPv6 probe failed; keeping ALL configured IPv6 routes, locally rejecting IPv6 TCP/UDP and filtering IPv6 DNS servers (" + result.detail + ")");
        return false;
    }

    private boolean isStopRequested() {
        synchronized (lock) {
            return stopRequested;
        }
    }

    /**
     * Turn the user's link config into the string gonc's -link expects. Android keeps
     * a stable local port for the lifetime of the VPN because tun2socks is started once.
     */
    private String resolveLinkConfig(String linkConfig) throws IOException {
        String clean = linkConfig == null ? "" : linkConfig.trim();
        if (clean.isEmpty()) {
            return "x://127.0.0.1:" + findAvailableLocalPort();
        }
        return clean;
    }

    private int normalizeMtu(int value) {
        return value < 576 || value > 9000 ? DEFAULT_MTU : value;
    }

    private static boolean isAllDigits(String value) {
        if (value.isEmpty()) {
            return false;
        }
        for (int i = 0; i < value.length(); i++) {
            if (!Character.isDigit(value.charAt(i))) {
                return false;
            }
        }
        return true;
    }

    /**
     * Best-effort local SOCKS5 endpoint ("host:port" tun2socks dials) derived from
     * the LEFT side of a gonc -link config "L;R" (',' is an alias for ';'). The left
     * side is gonc's local listener. Mirrors gonc's normalizeLinkConf/parseLinkConfig:
     * <ul>
     *   <li>a bare port ("3081") means {@code x://0.0.0.0:3081}, reached via loopback;</li>
     *   <li>an explicit scheme/host (x://, x+tls://, f://, optional user@ and ?query)
     *       is taken as-is, with a 0.0.0.0 / empty bind address mapped to 127.0.0.1
     *       since that is what tun2socks must connect to.</li>
     * </ul>
     */
    private static String socks5AddressFromLink(String link) {
        String left = link == null ? "" : link.trim();
        // gonc treats ',' and ';' as the L/R separator; keep the local (left) side.
        int sep = left.length();
        int semi = left.indexOf(';');
        if (semi >= 0) {
            sep = Math.min(sep, semi);
        }
        int comma = left.indexOf(',');
        if (comma >= 0) {
            sep = Math.min(sep, comma);
        }
        left = left.substring(0, sep).trim();

        if (left.isEmpty()) {
            return "127.0.0.1:1080";
        }
        if (isAllDigits(left)) {
            return "127.0.0.1:" + left; // x://0.0.0.0:<port>, dialed via loopback
        }
        // Strip scheme (x://, x+tls://, f://, …), then credentials and query.
        int scheme = left.indexOf("://");
        if (scheme >= 0) {
            left = left.substring(scheme + 3);
        }
        int query = left.indexOf('?');
        if (query >= 0) {
            left = left.substring(0, query);
        }
        int at = left.indexOf('@');
        if (at >= 0) {
            left = left.substring(at + 1);
        }
        left = left.trim();
        // A 0.0.0.0 / empty bind host is reached on the loopback by tun2socks.
        if (left.startsWith("0.0.0.0:")) {
            return "127.0.0.1:" + left.substring("0.0.0.0:".length());
        }
        if (left.startsWith(":")) {
            return "127.0.0.1" + left;
        }
        return left;
    }

    private int findAvailableLocalPort() throws IOException {
        InetAddress loopback = InetAddress.getByName("127.0.0.1");
        IOException lastError = null;
        int firstOffset = PORT_RANDOM.nextInt(AUTO_PORT_COUNT);
        for (int i = 0; i < AUTO_PORT_COUNT; i++) {
            int port = AUTO_PORT_START + ((firstOffset + i) % AUTO_PORT_COUNT);
            ServerSocket tcp = null;
            try {
                tcp = new ServerSocket(port, 50, loopback);
                tcp.setReuseAddress(false);
                return port;
            } catch (IOException error) {
                lastError = error;
            } finally {
                if (tcp != null) {
                    try {
                        tcp.close();
                    } catch (IOException ignored) {
                    }
                }
            }
        }
        throw lastError == null
                ? new IOException("Cannot find available local proxy port in " + AUTO_PORT_START + "-" + (AUTO_PORT_START + AUTO_PORT_COUNT - 1))
                : lastError;
    }

    private void closeDetachedFd(int fd) {
        if (fd < 0) {
            return;
        }
        try {
            ParcelFileDescriptor.adoptFd(fd).close();
        } catch (Exception ignored) {
        }
    }

    private void closeParcelQuietly(ParcelFileDescriptor descriptor) {
        try {
            if (descriptor != null) {
                descriptor.close();
            }
        } catch (Exception ignored) {
        }
    }

    private ParcelFileDescriptor establishInterface(boolean enableIpv6, List<String> dnsServers, String routeCidrs, int mtu) throws Exception {
        GoncCrashReporter.stage(this, "vpn builder create");
        Builder builder = new Builder();

        GoncCrashReporter.stage(this, "vpn builder set session");
        builder.setSession(getString(R.string.vpn_session_name));

        GoncCrashReporter.stage(this, "vpn builder set mtu");
        builder.setMtu(mtu);

        GoncCrashReporter.stage(this, "vpn builder add ipv4 address");
        builder.addAddress("10.60.173.33", 32);

        if (enableIpv6) {
            GoncCrashReporter.stage(this, "vpn builder add ipv6 address");
            builder.addAddress("fd60:173:33::2", 128);
        } else if (!disconnectProtection) {
            // Without an IPv6 route this family is intentionally outside the VPN.
            builder.allowFamily(OsConstants.AF_INET6);
        }

        addRoutes(builder, routeCidrs);
        addDnsServers(builder, dnsServers);
        try {
            GoncCrashReporter.stage(this, "vpn builder disallow self");
            builder.addDisallowedApplication(getPackageName());
        } catch (PackageManager.NameNotFoundException ignored) {
            if (disconnectProtection) throw new IllegalStateException("Cannot prepare VPN bypass", ignored);
            GoncCrashReporter.appendLog(this, "warn", "Cannot exclude self package from VPN");
        } catch (RuntimeException error) {
            if (disconnectProtection) throw new IllegalStateException("Cannot prepare VPN bypass", error);
            GoncCrashReporter.recordNonFatal(this, "Cannot exclude self package from VPN", error);
            log("warn", "Cannot exclude self package from VPN: " + (error.getMessage() == null ? error.toString() : error.getMessage()));
        }

        GoncCrashReporter.stage(this, "vpn builder establish begin");
        ParcelFileDescriptor descriptor = builder.establish();
        GoncCrashReporter.stage(this, descriptor == null ? "vpn builder establish returned null" : "vpn builder establish success");
        return descriptor;
    }

    private void addRoutes(Builder builder, String routeCidrs) {
        for (VpnRoutePolicy.Route route : VpnRoutePolicy.routes(routeCidrs)) {
            // A failed rule aborts setup. Never silently publish partial readiness.
            builder.addRoute(route.address, route.prefix);
        }
    }

    private void addDnsServers(Builder builder, List<String> dnsServers) {
        if (dnsServers.isEmpty()) throw new IllegalArgumentException("No usable VPN DNS servers");
        for (String dns : dnsServers) {
            InetAddress address = VpnRoutePolicy.numericAddress(dns);
            builder.addDnsServer(address);
            // DNS must stay inside the tunnel even with split routes / fallback.
            builder.addRoute(address, address.getAddress().length * 8);
        }
    }

    private static String defaultDnsServers() {
        return "8.8.8.8\n2001:4860:4860::8888";
    }

    private static String defaultRouteCidrs() {
        return "0.0.0.0/1\n128.0.0.0/1\n::/0";
    }

    private static String[] linesOrDefault(String value, String fallback) {
        String source = value == null || value.trim().isEmpty() ? fallback : value;
        return source.trim().split("\\r?\\n");
    }

    private Callback vpnCallback(TunnelReadySignal tunnelReady, boolean tunnelOnly) {
        return new Callback() {
            @Override
            public void event(String level, String message) {
                log(level == null ? "info" : level, message == null ? "" : message);
                if (message != null && !message.trim().isEmpty()) {
                    lastGoncMessage = message.trim();
                }
            }

            @Override
            public void p2PReport(String topic, String side, String status, String network, String mode, String peer, long timestamp, long pid) {
                if (isStopRequested()) return;
                GoncVpnState.setP2PReport(status, network, mode, peer);
                log("info", "P2P " + emptyDash(status) + " " + emptyDash(network) + " " + emptyDash(mode) + " " + emptyDash(peer));
                if (isStopRequested()) {
                    return;
                }
                updateNotification(notificationStatusText());
            }

            @Override
            public void traffic(String side, long inBytes, long outBytes, double inBps, double outBps, long elapsed, long connCount, boolean isFinal) {
                if (isStopRequested()) return;
                GoncVpnState.setTraffic(inBytes, outBytes, inBps, outBps);
            }

            @Override
            public void ready(String endpoint) {
                if (isStopRequested()) return;
                if (endpoint != null && endpoint.startsWith("socks5://")) {
                    GoncVpnState.setEndpoint(endpoint);
                    tunnelReady.markReady(endpoint);
                    if (tunnelOnly) {
                        GoncVpnState.setStatus(GoncVpnState.CONNECTED);
                        updateNotification(notificationStatusText());
                    }
                }
            }

            @Override
            public void stopped(long exitCode) {
                log(exitCode == 0 ? "info" : "error", "VPN gonc session stopped with code " + exitCode);
                synchronized (lock) {
                    goncSession = null;
                }
                if (!stopRequested) {
                    GoncVpnState.setError(buildStopError(exitCode));
                    stopVpn();
                }
            }

            @Override
            public void error(String message) {
                GoncVpnState.setError(message);
                stopVpn();
            }
        };
    }

    private static final class TunnelReadySignal {
        private final CountDownLatch ready = new CountDownLatch(1);
        private volatile String endpoint = "";
        private long revision;

        synchronized void markReady(String nextEndpoint) {
            endpoint = nextEndpoint == null ? "" : nextEndpoint.trim();
            revision++;
            ready.countDown();
        }

        synchronized long revision() { return revision; }

        String awaitReadyEndpoint(long timeoutMs) throws InterruptedException {
            if (ready.await(timeoutMs, TimeUnit.MILLISECONDS)) {
                return endpoint == null ? "" : endpoint;
            }
            return "";
        }
    }

    private boolean hasActiveResourcesLocked() {
        return vpnInterface != null || goncSession != null || tun2socksRunning || vpnWorker != null;
    }

    /**
     * Compose a user-facing error for an unexpected tunnel stop. A non-zero exit
     * (e.g. bad extra args / link config rejected by gonc) gets the exit code plus
     * the last line gonc printed, so the failure isn't silent.
     */
    private String buildStopError(long exitCode) {
        if (exitCode == 0) {
            return getString(R.string.vpn_error_disconnected);
        }
        String base = getString(R.string.vpn_error_exit_code, exitCode);
        String detail = lastGoncMessage == null ? "" : lastGoncMessage.trim();
        if (detail.length() > 300) {
            detail = detail.substring(0, 300) + "…";
        }
        return detail.isEmpty() ? base : base + "\n" + detail;
    }

    private void markConnected() {
        synchronized (lock) {
            if (stopRequested) {
                return;
            }
        }
        GoncVpnState.setSystemVpnReady(true);
        updateNotification(notificationStatusText());
    }

    private String connectingText() {
        // yellow circle U+1F7E1
        return new String(Character.toChars(0x1F7E1)) + " " + getString(R.string.vpn_service_starting);
    }

    private String notificationStatusText() {
        String status = GoncVpnState.status();
        if (GoncVpnState.protectionWaiting()) return yellowText(R.string.vpn_protection_waiting);
        if (GoncVpnState.ERROR.equals(status)) {
            return redText(R.string.vpn_service_error);
        }
        if (GoncVpnState.STOPPING.equals(status)) {
            return yellowText(R.string.vpn_service_stopping);
        }
        String p2p = normalizeP2PStatus(GoncVpnState.p2pStatus());
        if (p2p.startsWith("error") || p2p.startsWith("failed")) {
            return redText(R.string.vpn_service_error);
        }
        if ("connected".equals(p2p)) {
            if (GoncVpnState.systemVpnReady()) {
                return greenText(R.string.vpn_status_connected);
            }
            if (GoncVpnState.tunnelOnly() && GoncVpnState.socksReady()) {
                return greenText(R.string.vpn_service_socks_ready);
            }
            return yellowText(R.string.vpn_service_starting);
        }
        if (GoncVpnState.serviceReady()) {
            if ("disconnected".equals(p2p) || "idle".equals(p2p) || "stopped".equals(p2p)) {
                return yellowText(R.string.vpn_service_disconnected);
            }
            return yellowText(R.string.vpn_service_reconnecting);
        }
        return yellowText(R.string.vpn_service_starting);
    }

    private String normalizeP2PStatus(String value) {
        return value == null ? "" : value.trim().toLowerCase(java.util.Locale.ROOT);
    }

    private String yellowText(int resId) {
        return new String(Character.toChars(0x1F7E1)) + " " + getString(resId);
    }

    private String greenText(int resId) {
        return new String(Character.toChars(0x1F7E2)) + " " + getString(resId);
    }

    private String redText(int resId) {
        return new String(Character.toChars(0x1F534)) + " " + getString(resId);
    }

    private void stopVpn() {
        stopVpn(true);
    }

    private void stopVpn(boolean stopSelfWhenDone) {
        boolean keepError = GoncVpnState.ERROR.equals(GoncVpnState.status()) && !GoncVpnState.cleanupPending();
        boolean nothingToStop;
        synchronized (lock) {
            if (stopWorkerRunning) {
                stopRequested = true;
                if (!keepError) {
                    GoncVpnState.setStatus(GoncVpnState.STOPPING);
                }
                return;
            }
            stopRequested = true;
            nothingToStop = !hasActiveResourcesLocked();
            if (releaseMulticast != null) {
                releaseMulticast.run();
                releaseMulticast = null;
            }
            if (!nothingToStop) {
                stopWorkerRunning = true;
            }
        }
        ProtectedTunnelAttempt attempt = protectedAttempt;
        if (attempt != null) attempt.retire();
        synchronized (lock) { lock.notifyAll(); }
        if (nothingToStop) {
            GoncCrashReporter.stage(this, "vpn stop no active resources");
            finishStopped(keepError, stopSelfWhenDone);
            return;
        }
        if (!keepError) {
            GoncVpnState.setStatus(GoncVpnState.STOPPING);
        }
        Thread worker = new Thread(() -> {
            if (disconnectProtection) {
                stopProtectedVpn(keepError, stopSelfWhenDone);
                return;
            }
            Thread starting;
            synchronized (lock) { starting = vpnWorker; }
            if (starting != null) {
                try { starting.join(); }
                catch (InterruptedException error) { Thread.currentThread().interrupt(); }
            }
            mobilegonc.Session session;
            ParcelFileDescriptor tun;
            boolean stopTun2Socks;
            synchronized (lock) {
                session = goncSession;
                goncSession = null;
                tun = vpnInterface;
                vpnInterface = null;
                stopTun2Socks = tun2socksRunning;
                tun2socksRunning = false;
            }
            try {
                GoncCrashReporter.stage(this, "stop tun2socks");
                if (stopTun2Socks) {
                    Mobilegonc.stopTun2Socks();
                    log("info", "tun2socks stopped");
                }
            } catch (Throwable error) {
                GoncCrashReporter.recordNonFatal(this, "Error stopping tun2socks", error);
                log("error", "Error stopping tun2socks: " + (error.getMessage() == null ? error.toString() : error.getMessage()));
            }
            try {
                GoncCrashReporter.stage(this, "stop gonc tunnel");
                if (session != null) {
                    session.stop();
                }
            } catch (Throwable error) {
                GoncCrashReporter.recordNonFatal(this, "Error stopping gonc", error);
                log("error", "Error stopping gonc: " + error.getMessage());
            }
            try {
                GoncCrashReporter.stage(this, "close VPN interface");
                if (tun != null) {
                    tun.close();
                }
            } catch (Exception error) {
                GoncCrashReporter.recordNonFatal(this, "Error closing VPN interface", error);
                log("error", "Error closing VPN interface: " + error.getMessage());
            }
            synchronized (lock) {
                stopWorkerRunning = false;
            }
            finishStopped(keepError, stopSelfWhenDone);
        }, "gonc-vpn-stop");
        worker.start();
    }

    private void stopProtectedVpn(boolean keepError, boolean stopSelfWhenDone) {
        StringBuilder failures = new StringBuilder();
        Thread starting;
        synchronized (lock) { starting = vpnWorker; }
        try {
            if (starting != null) starting.join(10000);
            if (starting != null && starting.isAlive()) {
                throw new IllegalStateException("Tunnel worker has not stopped; resources retained for retry");
            }
            mobilegonc.Session session;
            synchronized (lock) { session = goncSession; }
            if (session != null) {
                try {
                    session.stop();
                    if (!session.awaitDone(5000)) throw new IllegalStateException("gonc stop timed out");
                    synchronized (lock) { if (goncSession == session) goncSession = null; }
                } catch (Exception error) { failures.append("gonc: ").append(error).append("\n"); }
            }
            synchronized (engineLock) {
                try {
                    Mobilegonc.stopTun2SocksWithError();
                    synchronized (lock) { tun2socksRunning = false; }
                } catch (Exception error) { failures.append("tun2socks: ").append(error).append("\n"); }
                // Retain Java's fd if the engine failed to stop; its duplicated fd
                // may still be alive, so reporting full cleanup would be misleading.
                if (!tun2socksRunning) {
                    ParcelFileDescriptor tun;
                    synchronized (lock) { tun = vpnInterface; }
                    if (tun != null) {
                        try {
                            tun.close();
                            synchronized (lock) { if (vpnInterface == tun) vpnInterface = null; }
                        } catch (Exception error) { failures.append("TUN: ").append(error).append("\n"); }
                    }
                }
            }
        } catch (Exception error) {
            failures.append(error);
            if (error instanceof InterruptedException) Thread.currentThread().interrupt();
        }
        if (failures.length() > 0) {
            String message = getString(R.string.vpn_cleanup_failed) + "\n" + failures;
            log("error", message);
            GoncVpnState.setCleanupPending(message);
            updateNotification(redText(R.string.vpn_cleanup_failed));
            synchronized (lock) { stopWorkerRunning = false; }
            return;
        }
        synchronized (lock) { stopWorkerRunning = false; }
        finishStopped(keepError, stopSelfWhenDone);
    }

    private void finishStopped(boolean keepError, boolean stopSelfWhenDone) {
        GoncVpnState.clearProtection();
        GoncVpnState.setEndpoint("");
        if (!keepError) {
            GoncVpnState.setStatus(GoncVpnState.DISCONNECTED);
        }
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                stopForeground(Service.STOP_FOREGROUND_REMOVE);
            } else {
                stopForeground(true);
            }
        } catch (RuntimeException ignored) {
        }
        cancelNotification();
        if (stopSelfWhenDone) {
            stopSelf();
        }
    }

    private void cancelNotification() {
        NotificationManager manager = getSystemService(NotificationManager.class);
        if (manager != null) {
            manager.cancel(NOTIFICATION_ID);
        }
    }

    @Override
    public void onRevoke() {
        stopVpn();
    }

    @Override
    public void onDestroy() {
        stopVpn(false);
        super.onDestroy();
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return;
        }
        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                getString(R.string.vpn_notification_channel),
                NotificationManager.IMPORTANCE_LOW
        );
        NotificationManager manager = getSystemService(NotificationManager.class);
        if (manager != null) {
            manager.createNotificationChannel(channel);
        }
    }

    private Notification notification(String status) {
        Intent intent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(
                this,
                0,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE
        );
        return new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle(getString(R.string.app_name))
                .setContentText(status)
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentIntent(pendingIntent)
                .setOngoing(true)
                .build();
    }

    private void updateNotification(String status) {
        NotificationManager manager = getSystemService(NotificationManager.class);
        if (manager != null) {
            manager.notify(NOTIFICATION_ID, notification(status));
        }
    }

    private static String emptyDash(String value) {
        return value == null || value.trim().isEmpty() ? "-" : value.trim();
    }

    private void log(String level, String message) {
        GoncCrashReporter.appendLog(this, level, message);
        GoncVpnState.log(level, message);
    }
}
