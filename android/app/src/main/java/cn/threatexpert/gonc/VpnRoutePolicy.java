package cn.threatexpert.gonc;

import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.List;

/** Capture follows routes; reachability controls DNS filtering and local rejection. */
final class VpnRoutePolicy {
    static final String DEFAULT_ROUTES = "0.0.0.0/1\n128.0.0.0/1\n::/0";
    static final String DEFAULT_DNS = "8.8.8.8\n2001:4860:4860::8888";

    static final class Route {
        final InetAddress address;
        final int prefix;
        Route(InetAddress address, int prefix) { this.address = address; this.prefix = prefix; }
        boolean isIpv6() { return address.getAddress().length == 16; }
    }

    static String[] lines(String value, String fallback) {
        return (value == null || value.trim().isEmpty() ? fallback : value.trim()).split("\\s+");
    }

    // Safe while editing an incomplete profile. Connection validates every CIDR.
    static boolean hasIpv6(String value) {
        for (String line : lines(value, DEFAULT_ROUTES)) {
            if (line.contains(":") && line.contains("/")) return true;
        }
        return false;
    }

    static boolean rejectIpv6(String routes, boolean ipv6Available) {
        return hasIpv6(routes) && !ipv6Available;
    }

    static InetAddress numericAddress(String value) {
        if (!value.matches("[0-9a-fA-F:.]+") || (!value.contains(":") && !value.matches("[0-9.]+"))) {
            throw new IllegalArgumentException("An IP address is required: " + value);
        }
        try {
            InetAddress address = InetAddress.getByName(value);
            if (value.contains(":") && address.getAddress().length != 16) {
                throw new IllegalArgumentException("IPv4-mapped IPv6 is not supported: " + value);
            }
            return address;
        } catch (UnknownHostException e) {
            throw new IllegalArgumentException("Invalid IP address: " + value, e);
        }
    }

    static List<Route> routes(String value) {
        List<Route> result = new ArrayList<>();
        for (String cidr : lines(value, DEFAULT_ROUTES)) {
            String[] parts = cidr.split("/", -1);
            if (parts.length != 2) throw new IllegalArgumentException("Invalid VPN route: " + cidr);
            InetAddress address = numericAddress(parts[0]);
            int prefix;
            try { prefix = Integer.parseInt(parts[1]); }
            catch (NumberFormatException e) { throw new IllegalArgumentException("Invalid VPN route: " + cidr, e); }
            byte[] bytes = address.getAddress();
            if (prefix < 0 || prefix > bytes.length * 8) throw new IllegalArgumentException("Invalid VPN route: " + cidr);
            for (int bit = prefix; bit < bytes.length * 8; bit++) bytes[bit / 8] &= ~(1 << (7 - bit % 8));
            try { result.add(new Route(InetAddress.getByAddress(bytes), prefix)); }
            catch (UnknownHostException impossible) { throw new IllegalArgumentException(impossible); }
        }
        return result;
    }

    static List<String> dns(String value, boolean ipv6Available) {
        List<String> result = new ArrayList<>();
        for (String line : lines(value, DEFAULT_DNS)) {
            InetAddress addr = numericAddress(line);
            if (addr.isAnyLocalAddress() || addr.isMulticastAddress()) throw new IllegalArgumentException("Invalid VPN DNS: " + line);
            if (addr.getAddress().length == 16 && !ipv6Available) continue;
            if (!result.contains(line)) result.add(line);
        }
        if (result.isEmpty()) result.add("8.8.8.8");
        return result;
    }
}
