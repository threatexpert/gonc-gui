package cn.threatexpert.gonc;

import android.content.Context;
import android.net.wifi.WifiManager;

import java.util.function.Consumer;

final class VpnDiscovery {
    private VpnDiscovery() {}

    static String extraArgs(String extraArgs, boolean enabled) {
        String args = extraArgs == null ? "" : extraArgs.trim();
        return enabled ? (args + " -p2p-with-lan").trim() : args;
    }

    static Runnable acquireMulticast(Context context, boolean enabled, Consumer<String> warn) {
        if (!enabled || context == null) return null;
        try {
            Context appContext = context.getApplicationContext();
            if (appContext == null) appContext = context;
            WifiManager wifi = (WifiManager) appContext.getSystemService(Context.WIFI_SERVICE);
            if (wifi == null) {
                warn.accept("Wi-Fi multicast service is unavailable; continuing with public P2P");
                return null;
            }
            WifiManager.MulticastLock lease = wifi.createMulticastLock("gonc-vpn-lan-discovery");
            lease.setReferenceCounted(false);
            lease.acquire();
            return () -> {
                try {
                    if (lease.isHeld()) lease.release();
                } catch (RuntimeException ignored) {
                    // Android may have already released the lock during shutdown.
                }
            };
        } catch (RuntimeException error) {
            warn.accept("Unable to enable Wi-Fi multicast reception; continuing with public P2P: " + error.getMessage());
            return null;
        }
    }
}
