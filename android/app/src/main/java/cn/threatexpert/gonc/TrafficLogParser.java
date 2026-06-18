package cn.threatexpert.gonc;

import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

final class TrafficLogParser {
    private static final Pattern TRAFFIC_PATTERN = Pattern.compile(
            "\\bIN:\\s+[^\\r\\n]*?\\((\\d+)\\s+bytes\\),\\s+([0-9]+(?:\\.[0-9]+)?\\s+[A-Za-z]+)(?:/s)?\\s*\\|\\s*"
                    + "OUT:\\s+[^\\r\\n]*?\\((\\d+)\\s+bytes\\),\\s+([0-9]+(?:\\.[0-9]+)?\\s+[A-Za-z]+)(?:/s)?",
            Pattern.CASE_INSENSITIVE
    );

    private TrafficLogParser() {
    }

    static Traffic parse(String message) {
        if (message == null) {
            return null;
        }
        Matcher matcher = TRAFFIC_PATTERN.matcher(message);
        if (!matcher.find()) {
            return null;
        }
        return new Traffic(
                parseRate(matcher.group(2)),
                parseRate(matcher.group(4))
        );
    }

    static boolean isProgressLog(String message) {
        String compact = message == null ? "" : message.trim();
        if (compact.isEmpty()) {
            return true;
        }
        return parse(compact) != null;
    }

    static double parseRate(String text) {
        if (text == null) {
            return 0;
        }
        String[] parts = text.trim().split("\\s+");
        if (parts.length < 2) {
            return 0;
        }
        double value;
        try {
            value = Double.parseDouble(parts[0]);
        } catch (NumberFormatException error) {
            return 0;
        }
        String unit = parts[1].toLowerCase(Locale.ROOT);
        if ("kb".equals(unit) || "kib".equals(unit)) {
            return value * 1024;
        }
        if ("mb".equals(unit) || "mib".equals(unit)) {
            return value * 1024 * 1024;
        }
        if ("gb".equals(unit) || "gib".equals(unit)) {
            return value * 1024 * 1024 * 1024;
        }
        return value;
    }

    static final class Traffic {
        final double inBps;
        final double outBps;

        Traffic(double inBps, double outBps) {
            this.inBps = inBps;
            this.outBps = outBps;
        }
    }
}
