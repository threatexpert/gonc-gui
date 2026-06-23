package cn.threatexpert.gonc;

import java.util.ArrayList;
import java.util.List;

final class GoncVpnState {
    interface Listener {
        void onVpnStateChanged();

        void onVpnLog(LogEntry entry);
    }

    static final class LogEntry {
        final long id;
        final String level;
        final String message;

        LogEntry(long id, String level, String message) {
            this.id = id;
            this.level = level;
            this.message = message;
        }
    }

    static final String DISCONNECTED = "Disconnected";
    static final String CONNECTING = "Connecting";
    static final String CONNECTED = "Connected";
    static final String STOPPING = "Stopping";
    static final String ERROR = "Error";
    private static final int MAX_LOGS = 240;

    private static String status = DISCONNECTED;
    private static String endpoint = "";
    private static String error = "";
    private static String p2pStatus = "idle";
    private static String network = "-";
    private static String route = "-";
    private static String peer = "-";
    private static String profileName = "";
    private static Listener listener;
    private static long nextLogId;
    private static final List<LogEntry> logs = new ArrayList<>();

    private GoncVpnState() {
    }

    static synchronized void setListener(Listener nextListener) {
        listener = nextListener;
        if (listener != null) {
            listener.onVpnStateChanged();
        }
    }

    // Only clear the listener if it is still the one supplied. During an Activity
    // recreation (e.g. a configuration change) the new instance registers in
    // onCreate before the old instance reaches onDestroy; an unconditional clear
    // there would wipe the new, visible listener and freeze its UI updates.
    static synchronized void removeListener(Listener candidate) {
        if (listener == candidate) {
            listener = null;
        }
    }

    static synchronized String status() {
        return status;
    }

    static synchronized String endpoint() {
        return endpoint;
    }

    static synchronized String error() {
        return error;
    }

    static synchronized String p2pStatus() {
        return p2pStatus;
    }

    static synchronized String network() {
        return network;
    }

    static synchronized String route() {
        return route;
    }

    static synchronized String peer() {
        return peer;
    }

    static synchronized String profileName() {
        return profileName;
    }

    static synchronized boolean isRunning() {
        return CONNECTING.equals(status) || CONNECTED.equals(status) || STOPPING.equals(status);
    }

    static synchronized List<LogEntry> logsAfter(long lastId) {
        List<LogEntry> result = new ArrayList<>();
        for (LogEntry entry : logs) {
            if (entry.id > lastId) {
                result.add(entry);
            }
        }
        return result;
    }

    static void startConnecting(String nextProfileName) {
        Listener snapshot;
        synchronized (GoncVpnState.class) {
            status = CONNECTING;
            endpoint = "";
            error = "";
            // Match the file modules' TransferMetrics.reset() so the initial label
            // reads the same ("连接中" until gonc's first report arrives).
            p2pStatus = "starting";
            network = "-";
            route = "-";
            peer = "-";
            profileName = nextProfileName == null ? "" : nextProfileName.trim();
            snapshot = listener;
        }
        if (snapshot != null) {
            snapshot.onVpnStateChanged();
        }
    }

    static void setStatus(String nextStatus) {
        Listener snapshot;
        synchronized (GoncVpnState.class) {
            status = nextStatus;
            if (!ERROR.equals(nextStatus)) {
                error = "";
            }
            if (DISCONNECTED.equals(nextStatus)) {
                endpoint = "";
                p2pStatus = "idle";
                network = "-";
                route = "-";
                peer = "-";
                profileName = "";
            }
            snapshot = listener;
        }
        if (snapshot != null) {
            snapshot.onVpnStateChanged();
        }
    }

    static void setEndpoint(String nextEndpoint) {
        Listener snapshot;
        synchronized (GoncVpnState.class) {
            endpoint = nextEndpoint == null ? "" : nextEndpoint;
            snapshot = listener;
        }
        if (snapshot != null) {
            snapshot.onVpnStateChanged();
        }
    }

    static void setP2PReport(String nextStatus, String nextNetwork, String nextRoute, String nextPeer) {
        Listener snapshot;
        synchronized (GoncVpnState.class) {
            if (nextStatus != null && !nextStatus.trim().isEmpty()) {
                // Store gonc's report status verbatim; the shared UiKit.displayStatus /
                // connectionState do all interpretation (same path as the file modules).
                p2pStatus = nextStatus.trim();
            }
            if (nextNetwork != null && !nextNetwork.trim().isEmpty()) {
                network = nextNetwork.trim();
            }
            if (nextRoute != null && !nextRoute.trim().isEmpty()) {
                route = nextRoute.trim();
            }
            if (nextPeer != null && !nextPeer.trim().isEmpty()) {
                peer = nextPeer.trim();
            }
            snapshot = listener;
        }
        if (snapshot != null) {
            snapshot.onVpnStateChanged();
        }
    }

    static void setError(String message) {
        Listener snapshot;
        synchronized (GoncVpnState.class) {
            status = ERROR;
            error = message == null ? "" : message;
            snapshot = listener;
        }
        if (snapshot != null) {
            snapshot.onVpnStateChanged();
        }
    }

    static void log(String level, String message) {
        Listener snapshot;
        LogEntry entry;
        synchronized (GoncVpnState.class) {
            if (message == null || message.trim().isEmpty()) {
                return;
            }
            entry = new LogEntry(++nextLogId, level == null || level.trim().isEmpty() ? "info" : level, message);
            logs.add(entry);
            while (logs.size() > MAX_LOGS) {
                logs.remove(0);
            }
            snapshot = listener;
        }
        if (snapshot != null) {
            snapshot.onVpnLog(entry);
        }
    }
}
