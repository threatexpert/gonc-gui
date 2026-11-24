package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.*;

public class NearbyModelTest {
    @Test public void parsesStateFromTheGoService() {
        NearbyModel.State state = NearbyModel.State.parse("{\"nickname\":\"Pixel 8\",\"visible\":false,"
                + "\"quickReceive\":false,\"discovering\":true,\"port\":19732,"
                + "\"devices\":[{\"id\":\"p-abc\",\"nickname\":\"Office-PC\",\"deviceType\":\"desktop\","
                + "\"addresses\":[\"192.168.1.5:19732\"],\"peerId\":\"abc\",\"paired\":true,\"verified\":true,"
                + "\"quickReceive\":true,\"lastSeen\":\"2026-11-23T10:00:00Z\"},{\"nickname\":\"no id\"}],"
                + "\"peers\":[{\"peerId\":\"abc\",\"nickname\":\"Office-PC\",\"deviceType\":\"desktop\"}]}");
        assertEquals("Pixel 8", state.nickname);
        assertFalse(state.visible);
        assertFalse(state.quickReceive);
        assertTrue(state.discovering);
        assertEquals(1, state.devices.size());
        NearbyModel.Device device = state.devices.get(0);
        assertEquals("p-abc", device.id);
        assertEquals("Office-PC", device.displayName());
        assertTrue(device.paired);
        assertTrue(device.quickReceive);
        assertEquals(1, state.peers.size());
        assertEquals("abc", state.peers.get(0).peerId);
    }

    @Test public void malformedJsonGivesDefaults() {
        NearbyModel.State state = NearbyModel.State.parse("not json");
        assertTrue(state.visible);
        assertTrue(state.devices.isEmpty());
        NearbyModel.Transfer t = NearbyModel.Transfer.parse("{");
        assertEquals("", t.id);
        assertFalse(t.finished());
    }

    @Test public void parsesTransfersAndProgress() {
        NearbyModel.Transfer t = NearbyModel.Transfer.parse("{\"id\":\"t1\",\"direction\":\"receive\","
                + "\"peerName\":\"MacBook\",\"status\":\"ready\",\"items\":[{\"name\":\"a.txt\",\"isDir\":false,"
                + "\"size\":10},{\"name\":\"photos\",\"isDir\":true,\"size\":90}],\"fileCount\":4,"
                + "\"totalBytes\":100,\"doneBytes\":42,\"quick\":true,\"endpoint\":\"http://127.0.0.1:5000\"}");
        assertTrue(t.isReceive());
        assertFalse(t.isSend());
        assertEquals(2, t.items.size());
        assertTrue(t.items.get(1).isDir);
        assertEquals(42, t.percent());
        assertTrue(t.quick);
        assertEquals("http://127.0.0.1:5000", t.endpoint);
        assertFalse(t.finished());
        for (String done : new String[]{"completed", "declined", "cancelled", "failed"}) {
            t.status = done;
            assertTrue(done, t.finished());
        }
        t.totalBytes = 0;
        t.status = "transferring";
        assertEquals(-1, t.percent());
    }

    @Test public void formatsThePairingCodeForComparison() {
        NearbyModel.Pairing p = NearbyModel.Pairing.parse("{\"id\":\"x\",\"incoming\":true,"
                + "\"peerName\":\"Office-PC\",\"code\":\"123456\",\"status\":\"waiting\"}");
        assertTrue(p.incoming);
        assertEquals("123 456", p.formattedCode());
        assertEquals("waiting", p.status);
    }

    @Test public void sendMetricsFollowRunningSends() {
        NearbyModel.Transfer a = NearbyModel.Transfer.parse("{\"id\":\"a\",\"direction\":\"send\","
                + "\"peerName\":\"Office-PC\",\"status\":\"transferring\",\"doneBytes\":1000,\"bytesPerSecond\":500}");
        NearbyModel.Transfer b = NearbyModel.Transfer.parse("{\"id\":\"b\",\"direction\":\"send\","
                + "\"peerName\":\"MacBook\",\"status\":\"waiting\"}");
        NearbyModel.Transfer failed = NearbyModel.Transfer.parse("{\"id\":\"f\",\"direction\":\"send\","
                + "\"status\":\"failed\",\"errorCode\":\"not-paired\"}");
        assertEquals("not-paired", failed.errorCode);
        assertEquals("", a.errorCode);
        NearbyModel.Transfer r = NearbyModel.Transfer.parse("{\"id\":\"r\",\"direction\":\"receive\","
                + "\"status\":\"transferring\",\"doneBytes\":9999,\"bytesPerSecond\":9999}");
        TransferMetrics m = new TransferMetrics();

        NearbyModel.applySendMetrics(m, java.util.Arrays.asList(a, b, r), 42);
        assertEquals(1000, m.outBytes);
        assertEquals(500, m.outBps, 0);
        assertEquals(42, m.lastTrafficMs);
        assertEquals(1, m.connectedCount);
        assertEquals(1, m.connectingCount);
        assertEquals("connected", m.p2pStatus);

        a.status = "completed";
        b.status = "declined";
        NearbyModel.applySendMetrics(m, java.util.Arrays.asList(a, b, r), 43);
        assertEquals(0, m.outBps, 0);
        assertEquals(0, m.lastTrafficMs);
        assertEquals(0, m.connectedCount);
        assertEquals("idle", m.p2pStatus);
        assertEquals("last send's total stays visible", 1000, m.outBytes);
    }

    @Test public void silentDevicesFadeTowardHalf() {
        NearbyModel.State state = NearbyModel.State.parse("{\"deviceFadeMs\":7000,\"deviceExpiryMs\":16000,"
                + "\"devices\":[{\"id\":\"a\",\"status\":\"fading\",\"lastSeen\":\"2026-11-24T08:33:50.5+09:00\"},"
                + "{\"id\":\"b\",\"lastSeen\":\"0001-01-01T00:00:00Z\"}]}");
        assertEquals(7000, state.deviceFadeMs);
        assertEquals(16000, state.deviceExpiryMs);
        NearbyModel.Device a = state.devices.get(0);
        NearbyModel.Device b = state.devices.get(1);
        assertTrue(a.fading());
        assertEquals(java.time.Instant.parse("2026-11-23T23:33:50.500Z").toEpochMilli(), a.lastSeenMs);
        assertFalse("status defaults to online", b.fading());
        assertEquals("Go's zero time means unknown", 0, b.lastSeenMs);

        long seen = a.lastSeenMs;
        assertEquals(1f, NearbyModel.vitality(a, seen + 7000, 7000, 16000), 0.001);
        assertEquals(0.5f, NearbyModel.vitality(a, seen + 14400, 7000, 16000), 0.001);
        assertEquals(0.75f, NearbyModel.vitality(a, seen + 10700, 7000, 16000), 0.001);
        assertEquals("never below half", 0.5f, NearbyModel.vitality(a, seen + 60000, 7000, 16000), 0.001);
        assertEquals("online devices stay solid", 1f, NearbyModel.vitality(b, seen + 60000, 7000, 16000), 0.001);
        assertEquals(9, NearbyModel.secondsSilent(a, seen + 9200));
        assertEquals("at least one second", 1, NearbyModel.secondsSilent(a, seen));
        assertEquals(0, NearbyModel.parseTime("not a time"));
    }

    @Test public void awayPairingsAreOrderedByLastSeen() {
        NearbyModel.State state = NearbyModel.State.parse("{\"peers\":["
                + "{\"peerId\":\"old\",\"pairedAt\":\"2026-01-01T00:00:00Z\",\"lastSeen\":\"2026-02-01T00:00:00Z\"},"
                + "{\"peerId\":\"never\",\"pairedAt\":\"2026-11-20T00:00:00Z\",\"lastSeen\":\"0001-01-01T00:00:00Z\"},"
                + "{\"peerId\":\"recent\",\"pairedAt\":\"2025-06-01T00:00:00Z\",\"lastSeen\":\"2026-11-23T00:00:00Z\"}]}");
        assertEquals(0, state.peers.get(1).lastSeenMs);
        java.util.List<NearbyModel.Peer> sorted = NearbyModel.byLastSeen(state.peers);
        assertEquals("recent", sorted.get(0).peerId);
        assertEquals("never seen: ordered by when it was paired", "never", sorted.get(1).peerId);
        assertEquals("old", sorted.get(2).peerId);
    }
}
