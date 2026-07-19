package cn.threatexpert.gonc;

import org.junit.Test;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

public class ReceivedTargetRefreshTest {
    @Test
    public void collectsTargetsThatBecomeVisibleOnARepeatedProviderQuery() {
        AtomicInteger calls = new AtomicInteger();
        Map<String, String> settled = ReceivedTargetRefresh.collect(
                2,
                () -> {
                    Map<String, String> found = new LinkedHashMap<>();
                    found.put("one.txt", "one");
                    if (calls.incrementAndGet() >= 2) {
                        found.put("two.txt", "two");
                    }
                    return found;
                },
                delayMs -> {
                });

        assertEquals(2, calls.get());
        assertEquals("one", settled.get("one.txt"));
        assertEquals("two", settled.get("two.txt"));
    }

    @Test
    public void boundsRetriesWhenSomeRemoteFilesAreNotLocal() {
        AtomicInteger calls = new AtomicInteger();
        Map<String, String> settled = ReceivedTargetRefresh.collect(
                2,
                () -> {
                    calls.incrementAndGet();
                    return Collections.singletonMap("one.txt", "one");
                },
                delayMs -> {
                });

        assertEquals(ReceivedTargetRefresh.MAX_ATTEMPTS, calls.get());
        assertEquals(Collections.singletonMap("one.txt", "one"), settled);
        assertTrue(calls.get() > 1);
    }
}
