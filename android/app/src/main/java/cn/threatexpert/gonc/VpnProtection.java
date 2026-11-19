package cn.threatexpert.gonc;

import java.net.InetAddress;
import java.util.ArrayList;
import java.util.List;

/** Argument policy shared by the confirmation UI and the service. */
final class VpnProtection {
    static final class Options {
        final String dns, arguments;
        final boolean hadDns, hasLocal;
        Options(String dns, String arguments, boolean hadDns, boolean hasLocal) {
            this.dns = dns; this.arguments = arguments;
            this.hadDns = hadDns; this.hasLocal = hasLocal;
        }
    }

    static Options options(String extra, String configuredDns) {
        String dns = normalizeDns(configuredDns);
        List<String> words = words(extra == null ? "" : extra);
        List<String> kept = new ArrayList<>();
        boolean hadDns = false, hasLocal = false;
        for (int i = 0; i < words.size(); i++) {
            String word = words.get(i);
            if (word.equals("--")) throw new IllegalArgumentException("Disconnect protection does not accept -- in advanced arguments");
            if (word.equals("-dns") || word.equals("--dns")) {
                hadDns = true;
                if (++i == words.size()) throw new IllegalArgumentException("Missing -dns value");
            } else if (word.startsWith("-dns=") || word.startsWith("--dns=")) {
                hadDns = true;
            } else {
                if (word.equals("-local") || word.equals("--local") || word.startsWith("-local=") || word.startsWith("--local=")) hasLocal = true;
                kept.add(word);
            }
        }
        kept.add("-dns"); kept.add(dns);
        StringBuilder args = new StringBuilder();
        for (String word : kept) {
            if (args.length() > 0) args.append(' ');
            args.append('\'').append(word.replace("'", "'\"'\"'")).append('\'');
        }
        return new Options(dns, args.toString(), hadDns, hasLocal);
    }

    static String normalizeDns(String value) {
        String input = value == null || value.trim().isEmpty() ? "8.8.8.8" : value.trim();
        String host = input, port = "53";
        if (input.startsWith("[")) {
            int end = input.indexOf(']');
            if (end < 0) throw new IllegalArgumentException("Invalid reconnect DNS");
            host = input.substring(1, end);
            String rest = input.substring(end + 1);
            if (!rest.isEmpty()) {
                if (!rest.startsWith(":")) throw new IllegalArgumentException("Invalid reconnect DNS");
                port = rest.substring(1);
            }
        } else if (input.indexOf(':') == input.lastIndexOf(':') && input.contains(":")) {
            host = input.substring(0, input.indexOf(':'));
            port = input.substring(input.indexOf(':') + 1);
        }
        InetAddress address = VpnRoutePolicy.numericAddress(host);
        if (address.isAnyLocalAddress() || address.isLoopbackAddress() || address.isMulticastAddress() || address.isLinkLocalAddress()) {
            throw new IllegalArgumentException("Reconnect DNS must be a reachable non-loopback DNS server IP");
        }
        int number;
        try { number = Integer.parseInt(port); }
        catch (NumberFormatException error) { throw new IllegalArgumentException("Invalid reconnect DNS port"); }
        if (number < 1 || number > 65535) throw new IllegalArgumentException("Invalid reconnect DNS port");
        return (host.contains(":") ? "[" + host + "]" : host) + ":" + number;
    }

    // Match goncembed.splitExtraArgs: adjacent quoted runs join; backslashes
    // are literal (not shell escapes). Reject incomplete quotes before starting.
    static List<String> words(String input) {
        List<String> result = new ArrayList<>();
        StringBuilder word = new StringBuilder();
        char quote = 0;
        boolean present = false;
        for (int i = 0; i < input.length(); i++) {
            char c = input.charAt(i);
            if (quote != 0) {
                if (c == quote) quote = 0; else word.append(c);
            } else if (c == '\'' || c == '"') {
                quote = c; present = true;
            } else if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                if (present) { result.add(word.toString()); word.setLength(0); present = false; }
            } else { word.append(c); present = true; }
        }
        if (quote != 0) throw new IllegalArgumentException("Unclosed quote in advanced arguments");
        if (present) result.add(word.toString());
        return result;
    }
}
