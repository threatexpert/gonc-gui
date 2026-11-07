package cn.threatexpert.gonc;

import org.json.JSONObject;
import org.junit.Test;

import static org.junit.Assert.*;

public class VpnQrPayloadTest {
    private JSONObject config() throws Exception {
        return new JSONObject().put("type", VpnQrPayload.TYPE).put("version", 1)
                .put("profile", new JSONObject().put("name", "Test")
                        .put("passphrase", "Abc12345").put("mtu", 1400).put("useUdp", false));
    }

    private void invalid(String value) {
        VpnQrPayload parsed = VpnQrPayload.parse(value);
        assertEquals(VpnQrPayload.Kind.INVALID, parsed.kind);
        assertNull(parsed.passphrase);
        assertNull(parsed.profile);
    }

    @Test public void ordinaryAndCustomPassphrasesRemainCompatible() {
        for (String value : new String[]{"Abc12345", "custom-passphrase!", "自定义口令123"}) {
            VpnQrPayload parsed = VpnQrPayload.parse("  " + value + "  ");
            assertEquals(VpnQrPayload.Kind.PASSPHRASE, parsed.kind);
            assertEquals(value, parsed.passphrase);
            assertNull(parsed.profile);
        }
    }

    @Test public void exportedProfileIsRecognized() throws Exception {
        VpnQrPayload parsed = VpnQrPayload.parse(config().toString());
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        assertEquals("Abc12345", parsed.profile.getString("passphrase"));
        assertNull(parsed.passphrase);
    }

    @Test public void exportedProfilePreservesWindowsDnsLeakPreference() throws Exception {
        VpnProfile profile = VpnProfile.defaults("Test");
        profile.passphrase = "Abc12345";
        assertFalse(profile.toJson().getBoolean("blockDnsLeak"));
        profile.blockDnsLeak = true;
        JSONObject root = config().put("profile", profile.toJson());
        VpnQrPayload parsed = VpnQrPayload.parse(root.toString());
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        assertTrue(parsed.profile.getBoolean("blockDnsLeak"));
        assertEquals(1, root.getInt("version"));
    }

    @Test public void exportedProfilePreservesOptInLanDiscovery() throws Exception {
        VpnProfile profile = VpnProfile.defaults("Test");
        profile.passphrase = "Abc12345";
        assertFalse(profile.toJson().getBoolean("p2pWithLan"));
        profile.p2pWithLan = true;
        VpnQrPayload parsed = VpnQrPayload.parse(config().put("profile", profile.toJson()).toString());
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        assertTrue(parsed.profile.getBoolean("p2pWithLan"));
    }

    @Test public void emptyInputIsRejected() {
        invalid(null);
        invalid("   ");
    }

    @Test public void damagedConfigurationNeverBecomesPassphrase() throws Exception {
        invalid("{broken");
        invalid("[1,2]");
        invalid("broken gonc.vpn.profile payload");
        invalid(config().toString() + " trailing junk");
        invalid("{}");
    }

    @Test public void unknownTypeIsRejected() throws Exception {
        invalid(config().put("type", "another.profile").toString());
    }

    @Test public void unsupportedVersionIsDistinctAndNotPassphrase() throws Exception {
        VpnQrPayload parsed = VpnQrPayload.parse(config().put("version", 2).toString());
        assertEquals(VpnQrPayload.Kind.UNSUPPORTED_VERSION, parsed.kind);
        assertNull(parsed.passphrase);
    }

    @Test public void legacyDesktopProfileWithoutVersionIsAccepted() throws Exception {
        JSONObject root = config();
        root.remove("version");
        JSONObject profile = root.getJSONObject("profile");
        profile.put("dnsServers", "8.8.8.8\n1.1.1.1");
        profile.put("routeCidrs", "0.0.0.0/1\n128.0.0.0/1\n::/0");
        profile.put("routeMetric", 1);
        profile.put("blockDnsLeak", true);
        profile.put("p2pWithLan", true);
        VpnQrPayload parsed = VpnQrPayload.parse(root.toString());
        assertEquals(VpnQrPayload.Kind.PROFILE, parsed.kind);
        assertEquals("Abc12345", parsed.profile.getString("passphrase"));
        assertTrue(parsed.profile.getBoolean("blockDnsLeak"));
        assertTrue(parsed.profile.getBoolean("p2pWithLan"));
    }

    @Test public void invalidExplicitVersionIsRejected() throws Exception {
        invalid(config().put("version", "1").toString());
        invalid(config().put("version", 1.5).toString());
        invalid(config().put("version", JSONObject.NULL).toString());
    }

    @Test public void legacyProfileStillRequiresValidFields() throws Exception {
        JSONObject root = config();
        root.remove("version");
        root.getJSONObject("profile").put("passphrase", " ");
        invalid(root.toString());
        root.getJSONObject("profile").put("passphrase", "Abc12345").put("mtu", "1400");
        invalid(root.toString());
    }

    @Test public void missingProfileOrPassphraseIsRejected() throws Exception {
        invalid(config().put("profile", "bad").toString());
        invalid(config().put("profile", new JSONObject()).toString());
        for (Object value : new Object[]{"  ", 123, JSONObject.NULL}) {
            JSONObject root = config();
            root.getJSONObject("profile").put("passphrase", value);
            invalid(root.toString());
        }
    }

    @Test public void wrongOptionalFieldTypesAreRejected() throws Exception {
        for (String key : new String[]{"name", "dnsServers", "routeCidrs", "linkConfig", "socks5Address", "extraArgs", "useUdp", "routeIpv6", "tunnelOnly", "blockDnsLeak", "p2pWithLan", "mtu"}) {
            JSONObject root = config();
            root.getJSONObject("profile").put(key, new JSONObject());
            invalid(root.toString());
        }
    }

    @Test public void invalidMtuIsRejected() throws Exception {
        for (Object value : new Object[]{0, 9001, 1400.5, "1400"}) {
            JSONObject root = config();
            root.getJSONObject("profile").put("mtu", value);
            invalid(root.toString());
        }
    }

    @Test public void optionalFieldsAndLegacyLinkRemainCompatible() throws Exception {
        JSONObject root = config().put("profile", new JSONObject()
                .put("passphrase", "Abc12345").put("socks5Address", "3080"));
        assertEquals(VpnQrPayload.Kind.PROFILE, VpnQrPayload.parse(root.toString()).kind);
    }
}
