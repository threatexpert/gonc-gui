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

import androidx.core.app.NotificationCompat;

import java.io.IOException;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import mobilegonc.Callback;
import mobilegonc.Mobilegonc;

public final class GoncVpnService extends VpnService {
    static final String ACTION_START = "cn.threatexpert.gonc.START_VPN";
    static final String ACTION_STOP = "cn.threatexpert.gonc.STOP_VPN";
    static final String EXTRA_PASSWORD = "password";
    static final String EXTRA_USE_UDP = "use_udp";
    static final String EXTRA_ENABLE_IPV6 = "enable_ipv6";
    static final String EXTRA_DNS_SERVERS = "dns_servers";
    static final String EXTRA_ROUTE_CIDRS = "route_cidrs";
    static final String EXTRA_LINK_CONFIG = "link_config";
    static final String EXTRA_EXTRA_ARGS = "extra_args";
    static final String EXTRA_TUNNEL_ONLY = "tunnel_only";

    private static final int NOTIFICATION_ID = 2001;
    private static final String CHANNEL_ID = "gonc_vpn";
    private static final int MTU = 1400;

    private final Object lock = new Object();
    private ParcelFileDescriptor vpnInterface;
    private mobilegonc.Session goncSession;
    private boolean tun2socksRunning;
    private boolean stopRequested;
    private boolean stopWorkerRunning;
    private volatile String lastGoncMessage = "";

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
            boolean enableIpv6 = intent.getBooleanExtra(EXTRA_ENABLE_IPV6, false);
            String dnsServers = intent.getStringExtra(EXTRA_DNS_SERVERS);
            String routeCidrs = intent.getStringExtra(EXTRA_ROUTE_CIDRS);
            String linkConfig = intent.getStringExtra(EXTRA_LINK_CONFIG);
            String extraArgs = intent.getStringExtra(EXTRA_EXTRA_ARGS);
            boolean tunnelOnly = intent.getBooleanExtra(EXTRA_TUNNEL_ONLY, false);
            startVpn(password, useUdp, enableIpv6, dnsServers, routeCidrs, linkConfig, extraArgs, tunnelOnly);
            return START_STICKY;
        }
        return START_NOT_STICKY;
    }

    private void startVpn(String password, boolean useUdp, boolean enableIpv6, String dnsServers, String routeCidrs, String linkConfig, String extraArgs, boolean tunnelOnly) {
        synchronized (lock) {
            if (goncSession != null || vpnInterface != null || tun2socksRunning) {
                log("warn", "VPN is already running");
                return;
            }
            stopRequested = false;
        }
        lastGoncMessage = "";
        GoncVpnState.setStatus(GoncVpnState.CONNECTING);
        GoncVpnState.setPeerIpv6(tunnelOnly ? "-" : (enableIpv6 ? "waiting" : "disabled"));
        log("info", tunnelOnly ? "Starting SOCKS5 tunnel (tunnel only)" : "Starting VPN tunnel");

        Thread worker = new Thread(() -> {
            try {
                if (isStopRequested()) {
                    return;
                }
                GoncCrashReporter.stage(this, "resolve link config");
                String effectiveLink = resolveLinkConfig(linkConfig);
                String socks5Endpoint = "socks5://" + socks5AddressFromLink(effectiveLink);
                log("info", "Using link config " + effectiveLink);

                if (isStopRequested()) {
                    return;
                }
                GoncCrashReporter.stage(this, "start gonc tunnel");
                TunnelReadySignal tunnelReady = new TunnelReadySignal();
                mobilegonc.Session session = Mobilegonc.startP2PTunnel(password, useUdp, effectiveLink, extraArgs == null ? "" : extraArgs, vpnCallback(tunnelReady));
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
                    // job. We report "connected" once gonc reports the SOCKS5 endpoint ready.
                    GoncVpnState.setPeerIpv6("-");
                    return;
                }

                GoncCrashReporter.stage(this, "wait for SOCKS5 endpoint before VPN interface");
                String readySocks5Endpoint = waitForSocks5Ready(tunnelReady);
                if (readySocks5Endpoint.isEmpty()) {
                    return;
                }

                boolean effectiveIpv6 = resolveEffectiveIpv6(enableIpv6, readySocks5Endpoint);
                if (isStopRequested()) {
                    return;
                }

                GoncCrashReporter.stage(this, "establish VPN interface");
                ParcelFileDescriptor establishedTun = establishInterface(effectiveIpv6, dnsServers, routeCidrs);
                if (establishedTun == null) {
                    throw new IllegalStateException("Could not establish VPN interface");
                }
                synchronized (lock) {
                    if (stopRequested) {
                        closeParcelQuietly(establishedTun);
                        return;
                    }
                    vpnInterface = establishedTun;
                }

                GoncCrashReporter.stage(this, "detach TUN fd for tun2socks");
                int tun2socksFd = -1;
                boolean shouldStartTun2Socks;
                synchronized (lock) {
                    shouldStartTun2Socks = !stopRequested && vpnInterface == establishedTun;
                    if (shouldStartTun2Socks) {
                        tun2socksFd = establishedTun.detachFd();
                        vpnInterface = null;
                    }
                }
                if (!shouldStartTun2Socks) {
                    return;
                }
                GoncCrashReporter.stage(this, "start tun2socks");
                boolean startedTun2Socks = false;
                boolean shouldStopAfterStart = false;
                try {
                    Mobilegonc.startTun2Socks(tun2socksFd, readySocks5Endpoint, "tun0", MTU, "warn");
                    startedTun2Socks = true;
                    // Step 3: close the original low-numbered fd via bionic so Android's
                    // fdsan tables are properly updated. Go already cleared the PFD tag
                    // and moved the engine to a high-numbered fd (>= 512); the original
                    // fd is no longer needed and must be released cleanly.
                    closeDetachedFd(tun2socksFd);
                    synchronized (lock) {
                        tun2socksRunning = true;
                        shouldStopAfterStart = stopRequested;
                    }
                    log("info", "tun2socks started");
                } finally {
                    if (!startedTun2Socks) {
                        closeDetachedFd(tun2socksFd);
                    }
                }
                if (shouldStopAfterStart) {
                    stopVpn();
                }
            } catch (Throwable error) {
                GoncCrashReporter.recordNonFatal(this, "VPN start failed", error);
                log("error", error.getMessage() == null ? error.toString() : error.getMessage());
                GoncVpnState.setError(error.getMessage() == null ? error.toString() : error.getMessage());
                stopVpn();
            }
        }, "gonc-vpn-start");
        worker.start();
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

    private boolean resolveEffectiveIpv6(boolean requestedIpv6, String socks5Endpoint) {
        if (!requestedIpv6) {
            GoncVpnState.setPeerIpv6("disabled");
            return false;
        }
        GoncVpnState.setPeerIpv6("checking");
        log("info", "Checking remote IPv6 availability");
        RemoteIpv6Probe.Result result = RemoteIpv6Probe.check(socks5Endpoint, RemoteIpv6Probe.DEFAULT_TIMEOUT_MS);
        if (result.available) {
            GoncVpnState.setPeerIpv6("available");
            log("info", "Remote IPv6 is available via " + result.detail + "; enabling IPv6 routes");
            return true;
        }
        GoncVpnState.setPeerIpv6("unavailable");
        log("warn", "Remote IPv6 is unavailable; IPv6 routes disabled (" + result.detail + ")");
        return false;
    }

    private boolean isStopRequested() {
        synchronized (lock) {
            return stopRequested;
        }
    }

    /**
     * Turn the user's link config into the string gonc's -link expects. Blank picks a
     * free local port and yields {@code x://127.0.0.1:<port>}; anything else (a bare
     * port, which gonc accepts directly, or a full mux link) is passed through verbatim.
     */
    private String resolveLinkConfig(String linkConfig) throws IOException {
        String clean = linkConfig == null ? "" : linkConfig.trim();
        if (clean.isEmpty()) {
            return "x://127.0.0.1:" + findAvailableLocalPort();
        }
        return clean;
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
        for (int i = 0; i < 20; i++) {
            ServerSocket tcp = null;
            DatagramSocket udp = null;
            try {
                tcp = new ServerSocket(0, 50, loopback);
                tcp.setReuseAddress(false);
                int port = tcp.getLocalPort();
                udp = new DatagramSocket(port, loopback);
                udp.setReuseAddress(false);
                return port;
            } catch (IOException error) {
                lastError = error;
            } finally {
                if (udp != null) {
                    udp.close();
                }
                if (tcp != null) {
                    try {
                        tcp.close();
                    } catch (IOException ignored) {
                    }
                }
            }
        }
        throw lastError == null ? new IOException("Cannot find available local proxy port") : lastError;
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

    private ParcelFileDescriptor establishInterface(boolean enableIpv6, String dnsServers, String routeCidrs) throws Exception {
        GoncCrashReporter.stage(this, "vpn builder create");
        Builder builder = new Builder();

        GoncCrashReporter.stage(this, "vpn builder set session");
        builder.setSession(getString(R.string.vpn_session_name));

        GoncCrashReporter.stage(this, "vpn builder set mtu");
        builder.setMtu(MTU);

        GoncCrashReporter.stage(this, "vpn builder add ipv4 address");
        builder.addAddress("10.0.0.2", 32);

        if (enableIpv6) {
            GoncCrashReporter.stage(this, "vpn builder add ipv6 address");
            builder.addAddress("fd00::2", 128);
        }

        addRoutes(builder, routeCidrs, enableIpv6);
        addDnsServers(builder, dnsServers, enableIpv6);
        try {
            GoncCrashReporter.stage(this, "vpn builder disallow self");
            builder.addDisallowedApplication(getPackageName());
        } catch (PackageManager.NameNotFoundException ignored) {
            GoncCrashReporter.appendLog(this, "warn", "Cannot exclude self package from VPN");
        } catch (RuntimeException error) {
            GoncCrashReporter.recordNonFatal(this, "Cannot exclude self package from VPN", error);
            log("warn", "Cannot exclude self package from VPN: " + (error.getMessage() == null ? error.toString() : error.getMessage()));
        }

        GoncCrashReporter.stage(this, "vpn builder establish begin");
        ParcelFileDescriptor descriptor = builder.establish();
        GoncCrashReporter.stage(this, descriptor == null ? "vpn builder establish returned null" : "vpn builder establish success");
        return descriptor;
    }

    private void addRoutes(Builder builder, String routeCidrs, boolean enableIpv6) {
        for (String cidr : linesOrDefault(routeCidrs, defaultRouteCidrs())) {
            cidr = cidr == null ? "" : cidr.trim();
            if (cidr.isEmpty()) {
                continue;
            }
            int slash = cidr.indexOf('/');
            if (slash <= 0 || slash == cidr.length() - 1) {
                GoncCrashReporter.appendLog(this, "warn", "Ignoring invalid VPN route " + cidr);
                continue;
            }
            String address = cidr.substring(0, slash).trim();
            int prefix;
            try {
                prefix = Integer.parseInt(cidr.substring(slash + 1).trim());
            } catch (NumberFormatException error) {
                GoncCrashReporter.appendLog(this, "warn", "Ignoring invalid VPN route " + cidr);
                continue;
            }
            boolean ipv6 = address.contains(":");
            if (ipv6 && !enableIpv6) {
                continue;
            }
            try {
                GoncCrashReporter.stage(this, "vpn builder add route " + cidr);
                builder.addRoute(address, prefix);
            } catch (RuntimeException error) {
                GoncCrashReporter.appendLog(this, "warn", "Cannot add VPN route " + cidr + ": " + error.getMessage());
            }
        }
    }

    private void addDnsServers(Builder builder, String dnsServers, boolean enableIpv6) {
        for (String dns : linesOrDefault(dnsServers, defaultDnsServers())) {
            dns = dns == null ? "" : dns.trim();
            if (dns.isEmpty()) {
                continue;
            }
            if (dns.contains(":") && !enableIpv6) {
                continue;
            }
            try {
                GoncCrashReporter.stage(this, "vpn builder add dns " + dns);
                builder.addDnsServer(dns);
            } catch (RuntimeException error) {
                GoncCrashReporter.appendLog(this, "warn", "Cannot add VPN DNS " + dns + ": " + error.getMessage());
            }
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

    private Callback vpnCallback(TunnelReadySignal tunnelReady) {
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
                GoncVpnState.setP2PReport(status, network, mode, peer);
                log("info", "P2P " + emptyDash(status) + " " + emptyDash(network) + " " + emptyDash(mode) + " " + emptyDash(peer));
                if (isStopRequested()) {
                    return;
                }
                if ("connected".equalsIgnoreCase(status)) {
                    markConnected();
                } else {
                    // P2P link is not (yet) up: show "connecting" with a yellow dot.
                    updateNotification(connectingText());
                }
            }

            @Override
            public void traffic(String side, long inBytes, long outBytes, double inBps, double outBps, long elapsed, long connCount, boolean isFinal) {
                GoncVpnState.setTraffic(inBps, outBps);
            }

            @Override
            public void ready(String endpoint) {
                if (endpoint != null && endpoint.startsWith("socks5://")) {
                    GoncVpnState.setEndpoint(endpoint);
                    tunnelReady.markReady(endpoint);
                    markConnected();
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

        void markReady(String nextEndpoint) {
            endpoint = nextEndpoint == null ? "" : nextEndpoint.trim();
            ready.countDown();
        }

        String awaitReadyEndpoint(long timeoutMs) throws InterruptedException {
            if (ready.await(timeoutMs, TimeUnit.MILLISECONDS)) {
                return endpoint == null ? "" : endpoint;
            }
            return "";
        }
    }

    private boolean hasActiveResourcesLocked() {
        return vpnInterface != null || goncSession != null || tun2socksRunning;
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
        GoncVpnState.setStatus(GoncVpnState.CONNECTED);
        updateNotification(connectedText());
    }

    private String connectingText() {
        // yellow circle U+1F7E1
        return new String(Character.toChars(0x1F7E1)) + " " + getString(R.string.vpn_status_connecting);
    }

    private String connectedText() {
        // green circle U+1F7E2
        return new String(Character.toChars(0x1F7E2)) + " " + getString(R.string.vpn_status_connected);
    }

    private void stopVpn() {
        stopVpn(true);
    }

    private void stopVpn(boolean stopSelfWhenDone) {
        boolean keepError = GoncVpnState.ERROR.equals(GoncVpnState.status());
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
            if (!nothingToStop) {
                stopWorkerRunning = true;
            }
        }
        if (nothingToStop) {
            GoncCrashReporter.stage(this, "vpn stop no active resources");
            finishStopped(keepError, stopSelfWhenDone);
            return;
        }
        if (!keepError) {
            GoncVpnState.setStatus(GoncVpnState.STOPPING);
        }
        Thread worker = new Thread(() -> {
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

    private void finishStopped(boolean keepError, boolean stopSelfWhenDone) {
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
