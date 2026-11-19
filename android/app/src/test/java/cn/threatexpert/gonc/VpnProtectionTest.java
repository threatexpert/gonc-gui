package cn.threatexpert.gonc;

import org.json.JSONObject;
import org.junit.Test;
import java.util.Arrays;
import static org.junit.Assert.*;

public class VpnProtectionTest {
    @Test public void defaultsAndExplicitDnsPorts() {
        assertEquals("8.8.8.8:53", VpnProtection.options("", "").dns);
        assertEquals("9.9.9.9:5353", VpnProtection.normalizeDns("9.9.9.9:5353"));
        assertEquals("[2001:4860:4860::8888]:53", VpnProtection.normalizeDns("2001:4860:4860::8888"));
        assertEquals("[2001:4860:4860::8888]:5353", VpnProtection.normalizeDns("[2001:4860:4860::8888]:5353"));
        assertFalse(VpnProtection.options("", "").hasLocal);
    }

    @Test public void rejectDnsThatCannotServeBypass() {
        for (String value : new String[]{"dns.example.com", "127.0.0.53", "::1", "0.0.0.0", "224.0.0.1", "8.8.8.8:0", "8.8.8.8:65536", "[::1]junk"}) {
            assertThrows(value, IllegalArgumentException.class, () -> VpnProtection.normalizeDns(value));
        }
    }

    @Test public void replaceAllDnsFormsPreserveExplicitLocalAndQuotedValues() {
        VpnProtection.Options options = VpnProtection.options("--dns=1.1.1.1 -dns '9.9.9.9' --local=192.168.1.2:0 -e ':nc host 80'", "8.8.4.4");
        assertTrue(options.hadDns);
        assertTrue(options.hasLocal);
        assertEquals(Arrays.asList("--local=192.168.1.2:0", "-e", ":nc host 80", "-dns", "8.8.4.4:53"), VpnProtection.words(options.arguments));
        assertFalse(VpnProtection.options("-e ':nc -local 1.2.3.4:0'", "").hasLocal);
        assertEquals(Arrays.asList("-e", "a\\b'c", "-dns", "8.8.8.8:53"),
                VpnProtection.words(VpnProtection.options("-e \"a\\b'c\"", "").arguments));
        assertThrows(IllegalArgumentException.class, () -> VpnProtection.options("-- -dns 1.1.1.1", ""));
        assertThrows(IllegalArgumentException.class, () -> VpnProtection.options("-dns", ""));
    }

    @Test public void profileAndQrPreserveProtectionAndValidateTypes() throws Exception {
        VpnProfile profile = VpnProfile.defaults("test");
        assertFalse(profile.disconnectProtection);
        assertEquals("", profile.reconnectDns);
        profile.passphrase = "Abcd123456";
        profile.disconnectProtection = true;
        profile.reconnectDns = "9.9.9.9:5353";
        JSONObject root = new JSONObject().put("type", VpnQrPayload.TYPE).put("version", 1).put("profile", profile.toJson());
        VpnQrPayload parsed = VpnQrPayload.parse(root.toString());
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        assertTrue(parsed.profile.getBoolean("disconnectProtection"));
        assertEquals("9.9.9.9:5353", parsed.profile.getString("reconnectDns"));
        VpnProfile restored = VpnProfile.fromJson(parsed.profile, "default");
        assertTrue(restored.disconnectProtection);
        assertEquals(profile.reconnectDns, restored.reconnectDns);
        VpnProfile legacy = VpnProfile.fromJson(new JSONObject().put("passphrase", "Abcd123456"), "default");
        assertFalse(legacy.disconnectProtection);
        assertEquals("", legacy.reconnectDns);
        root.getJSONObject("profile").put("disconnectProtection", "true");
        assertEquals(VpnQrPayload.Kind.INVALID, VpnQrPayload.parse(root.toString()).kind);
        root.getJSONObject("profile").put("disconnectProtection", true).put("reconnectDns", 8);
        assertEquals(VpnQrPayload.Kind.INVALID, VpnQrPayload.parse(root.toString()).kind);
    }

    @Test public void protectionAndFailedCleanupKeepStopAvailable() {
        GoncVpnState.startConnecting("test", false);
        GoncVpnState.setSystemVpnReady(true);
        GoncVpnState.setProtectionWaiting(true, "offline");
        assertTrue(GoncVpnState.isRunning());
        assertTrue(GoncVpnState.protectionWaiting());
        assertFalse(GoncVpnState.systemVpnReady());
        GoncVpnState.setSystemVpnReady(true);
        assertFalse(GoncVpnState.protectionWaiting());
        GoncVpnState.setCleanupPending("close failed");
        assertTrue(GoncVpnState.isRunning());
        GoncVpnState.clearProtection();
        GoncVpnState.setStatus(GoncVpnState.DISCONNECTED);
        assertFalse(GoncVpnState.isRunning());
    }
}
