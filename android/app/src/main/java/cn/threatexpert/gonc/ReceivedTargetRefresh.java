package cn.threatexpert.gonc;

import java.util.LinkedHashMap;
import java.util.Map;

final class ReceivedTargetRefresh {
    static final int MAX_ATTEMPTS = 5;
    private static final long[] RETRY_DELAYS_MS = {100L, 200L, 400L, 800L};

    private ReceivedTargetRefresh() {
    }

    interface Lookup<T> {
        Map<String, T> find();
    }

    interface Waiter {
        void pause(long delayMs) throws InterruptedException;
    }

    static <T> Map<String, T> collect(
            int expectedCount, Lookup<T> lookup, Waiter waiter) {
        Map<String, T> collected = new LinkedHashMap<>();
        int targetCount = Math.max(0, expectedCount);
        for (int attempt = 0; attempt < MAX_ATTEMPTS; attempt++) {
            Map<String, T> found = lookup.find();
            if (found != null) {
                collected.putAll(found);
            }
            if (collected.size() >= targetCount || attempt == MAX_ATTEMPTS - 1) {
                break;
            }
            try {
                waiter.pause(RETRY_DELAYS_MS[attempt]);
            } catch (InterruptedException error) {
                Thread.currentThread().interrupt();
                break;
            }
        }
        return collected;
    }
}
