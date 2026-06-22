package cn.threatexpert.gonc;

import android.content.Context;

import org.json.JSONException;
import org.json.JSONObject;

/** A saved VPN client profile (passphrase + routing/DNS options). Shared, persistable. */
final class VpnProfile {
    static final String DEFAULT_VPN_DNS = "8.8.8.8\n2001:4860:4860::8888";
    static final String DEFAULT_VPN_ROUTES = "0.0.0.0/1\n128.0.0.0/1\n::/0";

    String name;
    String passphrase;
    boolean useUdp;
    boolean routeIpv6;
    String dnsServers;
    String routeCidrs;

    static VpnProfile defaults(String name) {
        VpnProfile profile = new VpnProfile();
        profile.name = name == null ? "" : name;
        profile.passphrase = "";
        profile.useUdp = false;
        profile.routeIpv6 = false;
        profile.dnsServers = DEFAULT_VPN_DNS;
        profile.routeCidrs = DEFAULT_VPN_ROUTES;
        return profile;
    }

    static VpnProfile fromJson(JSONObject object, Context context) {
        VpnProfile profile = defaults(context.getString(R.string.vpn_profile_default_name));
        profile.name = object.optString("name", profile.name);
        profile.passphrase = object.optString("passphrase", "");
        profile.useUdp = object.optBoolean("useUdp", false);
        profile.routeIpv6 = object.optBoolean("routeIpv6", false);
        profile.dnsServers = object.optString("dnsServers", DEFAULT_VPN_DNS);
        profile.routeCidrs = object.optString("routeCidrs", DEFAULT_VPN_ROUTES);
        return profile;
    }

    JSONObject toJson() {
        JSONObject object = new JSONObject();
        try {
            object.put("name", name == null ? "" : name);
            object.put("passphrase", passphrase == null ? "" : passphrase);
            object.put("useUdp", useUdp);
            object.put("routeIpv6", routeIpv6);
            object.put("dnsServers", dnsServers == null ? "" : dnsServers);
            object.put("routeCidrs", routeCidrs == null ? "" : routeCidrs);
        } catch (JSONException ignored) {
        }
        return object;
    }

    String displayName(Context context) {
        return name == null || name.trim().isEmpty()
                ? context.getString(R.string.vpn_profile_default_name)
                : name.trim();
    }
}
