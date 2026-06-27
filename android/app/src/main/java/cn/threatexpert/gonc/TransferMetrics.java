package cn.threatexpert.gonc;

import java.util.LinkedHashMap;
import java.util.Map;

/** Live transfer/session metrics for a module (shared across all modules). */
final class TransferMetrics {
    final Map<String, String> sessions = new LinkedHashMap<>();
    String p2pStatus = "idle";
    String network = "-";
    String routeMode = "-";
    String peer = "-";
    int connectedCount;
    int connectingCount;
    long inBytes;
    long outBytes;
    double inBps;
    double outBps;
    long lastTrafficMs;

    void reset() {
        sessions.clear();
        p2pStatus = "starting";
        network = "-";
        routeMode = "-";
        peer = "-";
        connectedCount = 0;
        connectingCount = 0;
        inBytes = 0;
        outBytes = 0;
        inBps = 0;
        outBps = 0;
        lastTrafficMs = 0;
    }

    void markStopped() {
        p2pStatus = "idle";
        connectedCount = 0;
        connectingCount = 0;
        inBytes = 0;
        outBytes = 0;
        inBps = 0;
        outBps = 0;
        lastTrafficMs = 0;
        routeMode = "-";
        sessions.clear();
    }

    void recountConnections() {
        connectedCount = 0;
        connectingCount = 0;
        for (String status : sessions.values()) {
            if ("connected".equals(status)) {
                connectedCount++;
            } else if ("connecting".equals(status)) {
                connectingCount++;
            }
        }
    }
}
