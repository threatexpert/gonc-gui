package cn.threatexpert.gonc;

import java.util.function.Consumer;

/** Consume cancellation and successful results alike; a scan can start only once. */
final class PendingQrScan {
    private Consumer<String> callback;
    boolean begin(Consumer<String> next) {
        if (callback != null) return false;
        callback = next;
        return true;
    }
    void cancel() { callback = null; }
    void complete(boolean success, String result) {
        Consumer<String> current = callback;
        callback = null;
        if (success && current != null && result != null && !result.trim().isEmpty()) current.accept(result.trim());
    }
}
