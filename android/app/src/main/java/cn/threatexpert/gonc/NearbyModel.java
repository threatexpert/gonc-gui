package cn.threatexpert.gonc;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Plain data parsed from the Go Nearby service's JSON events. Parsing never
 * throws: malformed input yields empty defaults so a bad event cannot crash
 * the UI.
 */
final class NearbyModel {
    private NearbyModel() {
    }

    static final class Device {
        String id = "";
        String nickname = "";
        String deviceType = "";
        String peerId = "";
        boolean paired;
        boolean quickReceive;
        /** "online", or "fading" once the device has stopped announcing itself. */
        String status = "online";
        /** When the device was last heard from, in epoch milliseconds, or 0. */
        long lastSeenMs;

        boolean fading() {
            return "fading".equals(status);
        }

        String displayName() {
            return nickname.isEmpty() ? id : nickname;
        }
    }

    static final class Peer {
        String peerId = "";
        String nickname = "";
        String deviceType = "";
        /** When the pairing was made and when the device was last nearby, in epoch ms, or 0. */
        long pairedAtMs;
        long lastSeenMs;
    }

    /**
     * Paired devices most recently seen first. Pairings not seen since they
     * were made are ordered by when they were made.
     */
    static List<Peer> byLastSeen(List<Peer> peers) {
        List<Peer> sorted = new ArrayList<>(peers);
        Collections.sort(sorted, (a, b) -> Long.compare(
                b.lastSeenMs > 0 ? b.lastSeenMs : b.pairedAtMs,
                a.lastSeenMs > 0 ? a.lastSeenMs : a.pairedAtMs));
        return sorted;
    }

    static final class State {
        String nickname = "";
        boolean visible = true;
        boolean quickReceive = true;
        boolean discovering;
        String discoveryError = "";
        /** See nearby.DeviceFadeAfter and nearby.DeviceExpiry. */
        long deviceFadeMs = 7000;
        long deviceExpiryMs = 16000;
        List<Device> devices = Collections.emptyList();
        List<Peer> peers = Collections.emptyList();

        static State parse(String json) {
            State state = new State();
            try {
                JSONObject o = new JSONObject(json);
                state.nickname = o.optString("nickname", "");
                state.visible = o.optBoolean("visible", true);
                state.quickReceive = o.optBoolean("quickReceive", true);
                state.discovering = o.optBoolean("discovering", false);
                state.discoveryError = o.optString("discoveryError", "");
                state.deviceFadeMs = o.optLong("deviceFadeMs", state.deviceFadeMs);
                state.deviceExpiryMs = o.optLong("deviceExpiryMs", state.deviceExpiryMs);
                List<Device> devices = new ArrayList<>();
                JSONArray list = o.optJSONArray("devices");
                for (int i = 0; list != null && i < list.length(); i++) {
                    JSONObject d = list.optJSONObject(i);
                    if (d == null) continue;
                    Device device = new Device();
                    device.id = d.optString("id", "");
                    device.nickname = d.optString("nickname", "");
                    device.deviceType = d.optString("deviceType", "");
                    device.peerId = d.optString("peerId", "");
                    device.paired = d.optBoolean("paired", false);
                    device.quickReceive = d.optBoolean("quickReceive", false);
                    device.status = d.optString("status", "online");
                    device.lastSeenMs = parseTime(d.optString("lastSeen", ""));
                    if (!device.id.isEmpty()) devices.add(device);
                }
                state.devices = devices;
                List<Peer> peers = new ArrayList<>();
                JSONArray peerList = o.optJSONArray("peers");
                for (int i = 0; peerList != null && i < peerList.length(); i++) {
                    JSONObject p = peerList.optJSONObject(i);
                    if (p == null) continue;
                    Peer peer = new Peer();
                    peer.peerId = p.optString("peerId", "");
                    peer.nickname = p.optString("nickname", "");
                    peer.deviceType = p.optString("deviceType", "");
                    peer.pairedAtMs = parseTime(p.optString("pairedAt", ""));
                    peer.lastSeenMs = parseTime(p.optString("lastSeen", ""));
                    if (!peer.peerId.isEmpty()) peers.add(peer);
                }
                state.peers = peers;
            } catch (JSONException ignored) {
                // Keep the defaults.
            }
            return state;
        }
    }

    /** Parses a Go time in JSON, such as 2026-11-24T08:33:57.7477666+09:00, or returns 0. */
    static long parseTime(String value) {
        if (value == null || value.isEmpty()) {
            return 0;
        }
        try {
            long ms = java.time.OffsetDateTime.parse(value).toInstant().toEpochMilli();
            return Math.max(0, ms);
        } catch (RuntimeException e) {
            return 0;
        }
    }

    /**
     * How present a device looks, from 1 down to 0.5. It stays at 1 while the
     * device announces itself normally; once the service marks it fading it
     * eases toward 0.5 at 90% of the time after which it leaves the list. The
     * desktop page uses the same rule.
     */
    static float vitality(Device d, long now, long fadeMs, long expiryMs) {
        if (!d.fading() || d.lastSeenMs <= 0) {
            return 1f;
        }
        long age = now - d.lastSeenMs;
        if (age <= fadeMs) {
            return 1f;
        }
        double end = Math.max(fadeMs + 1, expiryMs * 0.9);
        double p = Math.min(1.0, (age - fadeMs) / (end - fadeMs));
        return (float) (1.0 - 0.5 * p);
    }

    /** Whole seconds since the device was last heard from, at least 1. */
    static int secondsSilent(Device d, long now) {
        return (int) Math.max(1, Math.round((now - d.lastSeenMs) / 1000.0));
    }

    static final class Pairing {
        String id = "";
        boolean incoming;
        String peerName = "";
        String code = "";
        String status = "";
        String error = "";

        static Pairing parse(String json) {
            Pairing p = new Pairing();
            try {
                JSONObject o = new JSONObject(json);
                p.id = o.optString("id", "");
                p.incoming = o.optBoolean("incoming", false);
                p.peerName = o.optString("peerName", "");
                p.code = o.optString("code", "");
                p.status = o.optString("status", "");
                p.error = o.optString("error", "");
            } catch (JSONException ignored) {
                // Keep the defaults.
            }
            return p;
        }

        /** "123456" becomes "123 456", which is easier to compare aloud. */
        String formattedCode() {
            if (code.length() == 6) {
                return code.substring(0, 3) + " " + code.substring(3);
            }
            return code;
        }
    }

    static final class Item {
        String name = "";
        boolean isDir;
        long size;
    }

    /**
     * Folds the running sends into the Activity panel's send metrics: bytes
     * sent, speed, the peer and how many sends are moving data. The totals of
     * the last send stay visible after it ends.
     */
    static void applySendMetrics(TransferMetrics m, Iterable<Transfer> transfers, long now) {
        long done = 0;
        long bps = 0;
        int active = 0;
        int transferring = 0;
        String peer = "";
        for (Transfer t : transfers) {
            if (!t.isSend() || t.finished()) continue;
            active++;
            if (!t.peerName.isEmpty()) peer = t.peerName;
            if ("transferring".equals(t.status)) {
                transferring++;
                done += t.doneBytes;
                bps += t.bytesPerSecond;
            }
        }
        m.outBps = bps;
        if (active == 0) {
            m.p2pStatus = "idle";
            m.connectedCount = 0;
            m.connectingCount = 0;
            m.lastTrafficMs = 0;
            return;
        }
        m.peer = peer.isEmpty() ? "-" : peer;
        m.outBytes = done;
        m.connectedCount = transferring;
        m.connectingCount = active - transferring;
        m.p2pStatus = transferring > 0 ? "connected" : "waiting";
        m.lastTrafficMs = transferring > 0 ? now : 0;
    }

    static final class Transfer {
        String id = "";
        String direction = "";
        String deviceId = "";
        String peerName = "";
        String status = "";
        String message = "";
        /** not-paired, quick-refused, busy, timeout or empty; see nearbyapp. */
        String errorCode = "";
        List<Item> items = Collections.emptyList();
        long fileCount;
        long totalBytes;
        long doneBytes;
        long bytesPerSecond;
        boolean quick;
        String endpoint = "";

        static Transfer parse(String json) {
            Transfer t = new Transfer();
            try {
                JSONObject o = new JSONObject(json);
                t.id = o.optString("id", "");
                t.direction = o.optString("direction", "");
                t.deviceId = o.optString("deviceId", "");
                t.peerName = o.optString("peerName", "");
                t.status = o.optString("status", "");
                t.message = o.optString("message", "");
                t.errorCode = o.optString("errorCode", "");
                t.fileCount = o.optLong("fileCount", 0);
                t.totalBytes = o.optLong("totalBytes", 0);
                t.doneBytes = o.optLong("doneBytes", 0);
                t.bytesPerSecond = o.optLong("bytesPerSecond", 0);
                t.quick = o.optBoolean("quick", false);
                t.endpoint = o.optString("endpoint", "");
                List<Item> items = new ArrayList<>();
                JSONArray list = o.optJSONArray("items");
                for (int i = 0; list != null && i < list.length(); i++) {
                    JSONObject it = list.optJSONObject(i);
                    if (it == null) continue;
                    Item item = new Item();
                    item.name = it.optString("name", "");
                    item.isDir = it.optBoolean("isDir", false);
                    item.size = it.optLong("size", 0);
                    items.add(item);
                }
                t.items = items;
            } catch (JSONException ignored) {
                // Keep the defaults.
            }
            return t;
        }

        boolean isSend() {
            return "send".equals(direction);
        }

        boolean isReceive() {
            return "receive".equals(direction);
        }

        boolean finished() {
            return "completed".equals(status) || "declined".equals(status)
                    || "cancelled".equals(status) || "failed".equals(status);
        }

        /** 0..100, or -1 while the size is unknown. */
        int percent() {
            if (totalBytes <= 0) {
                return "completed".equals(status) ? 100 : -1;
            }
            long done = Math.max(0, Math.min(doneBytes, totalBytes));
            return (int) (done * 100 / totalBytes);
        }
    }
}
