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

    private static final int NOTIFICATION_ID = 2001;
    private static final String CHANNEL_ID = "gonc_vpn";
    private static final int MTU = 1400;

    private final Object lock = new Object();
    private ParcelFileDescriptor vpnInterface;
    private mobilegonc.Session goncSession;
    private boolean tun2socksRunning;
    private boolean stopRequested;
    private boolean stopWorkerRunning;

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
                startForeground(NOTIFICATION_ID, notification(getString(R.string.vpn_status_connecting)));
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
            startVpn(password, useUdp, enableIpv6, dnsServers, routeCidrs);
            return START_STICKY;
        }
        return START_NOT_STICKY;
    }

    private void startVpn(String password, boolean useUdp, boolean enableIpv6, String dnsServers, String routeCidrs) {
        synchronized (lock) {
            if (goncSession != null || vpnInterface != null || tun2socksRunning) {
                log("warn", "VPN is already running");
                return;
            }
            stopRequested = false;
        }
        GoncVpnState.setStatus(GoncVpnState.CONNECTING);
        log("info", "Starting VPN tunnel");

        ParcelFileDescriptor tun;
        try {
            GoncCrashReporter.stage(this, "establish VPN interface");
            tun = establishInterface(enableIpv6, dnsServers, routeCidrs);
            if (tun == null) {
                throw new IllegalStateException("Could not establish VPN interface");
            }
            synchronized (lock) {
                vpnInterface = tun;
            }
        } catch (Throwable error) {
            GoncCrashReporter.recordNonFatal(this, "VPN establish failed", error);
            log("error", error.getMessage() == null ? error.toString() : error.getMessage());
            GoncVpnState.setError(error.getMessage() == null ? error.toString() : error.getMessage());
            stopVpn();
            return;
        }

        ParcelFileDescriptor establishedTun = tun;
        Thread worker = new Thread(() -> {
            try {
                if (isStopRequested()) {
                    return;
                }
                GoncCrashReporter.stage(this, "select local proxy port");
                int socksPort = findAvailableLocalPort();
                log("info", "Selected local SOCKS5 port " + socksPort);
                GoncVpnState.setEndpoint("socks5://127.0.0.1:" + socksPort);

                if (isStopRequested()) {
                    return;
                }
                GoncCrashReporter.stage(this, "start gonc tunnel");
                mobilegonc.Session session = Mobilegonc.startP2PTunnel(password, useUdp, socksPort, "", vpnCallback());
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
                    Mobilegonc.startTun2Socks(tun2socksFd, "socks5://127.0.0.1:" + socksPort, "tun0", MTU, "warn");
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

    private boolean isStopRequested() {
        synchronized (lock) {
            return stopRequested;
        }
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

    private Callback vpnCallback() {
        return new Callback() {
            @Override
            public void event(String level, String message) {
                log(level == null ? "info" : level, message == null ? "" : message);
            }

            @Override
            public void p2PReport(String topic, String status, String network, String mode, String peer, long timestamp, long pid) {
                GoncVpnState.setP2PReport(status, network, mode, peer);
                log("info", "P2P " + emptyDash(status) + " " + emptyDash(network) + " " + emptyDash(mode) + " " + emptyDash(peer));
                if ("connected".equalsIgnoreCase(status)) {
                    markConnected();
                }
            }

            @Override
            public void ready(String endpoint) {
                if (endpoint != null && endpoint.startsWith("socks5://")) {
                    GoncVpnState.setEndpoint(endpoint);
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
                    GoncVpnState.setError("VPN tunnel disconnected");
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

    private boolean hasActiveResourcesLocked() {
        return vpnInterface != null || goncSession != null || tun2socksRunning;
    }

    private void markConnected() {
        synchronized (lock) {
            if (stopRequested) {
                return;
            }
        }
        GoncVpnState.setStatus(GoncVpnState.CONNECTED);
        updateNotification(getString(R.string.vpn_status_connected));
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
        if (stopSelfWhenDone) {
            stopSelf();
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
