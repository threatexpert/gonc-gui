package cn.threatexpert.gonc;

import org.json.JSONException;
import org.json.JSONObject;
import org.json.JSONTokener;

/** Classifies scanner input before it can mutate a profile or its passphrase. */
final class VpnQrPayload {
    static final String TYPE = "gonc.vpn.profile";
    enum Kind { PASSPHRASE, PROFILE, INVALID, UNSUPPORTED_VERSION }

    final Kind kind;
    final String passphrase;
    final JSONObject profile;

    private VpnQrPayload(Kind kind, String passphrase, JSONObject profile) {
        this.kind = kind;
        this.passphrase = passphrase;
        this.profile = profile;
    }

    static VpnQrPayload parse(String input) {
        String value = input == null ? "" : input.trim();
        if (value.isEmpty()) return result(Kind.INVALID);
        // Structured/malformed configuration data must never fall back to a password.
        if (!value.startsWith("{") && !value.startsWith("[") && !value.contains(TYPE)) {
            return new VpnQrPayload(Kind.PASSPHRASE, value, null);
        }
        try {
            JSONTokener tokens = new JSONTokener(value);
            Object decoded = tokens.nextValue();
            if (!(decoded instanceof JSONObject) || tokens.nextClean() != 0) return result(Kind.INVALID);
            JSONObject root = (JSONObject) decoded;
            if (!TYPE.equals(root.opt("type"))) return result(Kind.INVALID);
            // Earlier desktop exports omitted version; their profile format is version 1.
            if (root.has("version")) {
                Object version = root.opt("version");
                if (!(version instanceof Integer) && !(version instanceof Long)) return result(Kind.INVALID);
                if (((Number) version).longValue() != 1) return result(Kind.UNSUPPORTED_VERSION);
            }
            JSONObject profile = root.optJSONObject("profile");
            if (profile == null || !(profile.opt("passphrase") instanceof String)
                    || profile.getString("passphrase").trim().isEmpty()) return result(Kind.INVALID);
            for (String key : new String[]{"name", "dnsServers", "routeCidrs", "linkConfig", "socks5Address", "extraArgs"}) {
                if (profile.has(key) && !(profile.opt(key) instanceof String)) return result(Kind.INVALID);
            }
            for (String key : new String[]{"useUdp", "routeIpv6", "tunnelOnly", "blockDnsLeak", "p2pWithLan"}) {
                if (profile.has(key) && !(profile.opt(key) instanceof Boolean)) return result(Kind.INVALID);
            }
            if (profile.has("mtu")) {
                Object mtu = profile.opt("mtu");
                if (!(mtu instanceof Integer) && !(mtu instanceof Long)) return result(Kind.INVALID);
                long size = ((Number) mtu).longValue();
                if (size < 576 || size > 9000) return result(Kind.INVALID);
            }
            return new VpnQrPayload(Kind.PROFILE, null, profile);
        } catch (JSONException error) {
            return result(Kind.INVALID);
        }
    }

    private static VpnQrPayload result(Kind kind) {
        return new VpnQrPayload(kind, null, null);
    }
}
