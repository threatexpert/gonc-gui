package cn.threatexpert.gonc;

import org.junit.Test;
import java.util.ArrayList;
import java.util.List;
import static org.junit.Assert.*;

public class PendingQrScanTest {
    @Test public void cancelledAndDeniedScansDoNotStartTransfers() {
        PendingQrScan scan = new PendingQrScan();
        List<String> starts = new ArrayList<>();
        assertTrue(scan.begin(starts::add));
        scan.complete(false, "ignored123");
        scan.complete(true, "late1234");
        assertTrue(scan.begin(starts::add));
        scan.cancel();
        scan.complete(true, "denied123");
        assertTrue(starts.isEmpty());
    }
    @Test public void successfulScanStartsOnceAndCanBeUsedAgain() {
        PendingQrScan scan = new PendingQrScan();
        List<String> starts = new ArrayList<>();
        assertTrue(scan.begin(starts::add));
        assertFalse(scan.begin(value -> fail("overlapping scan")));
        scan.complete(true, " scanned123 ");
        scan.complete(true, "duplicate123");
        assertEquals(1, starts.size());
        assertEquals("scanned123", starts.get(0));
        assertTrue(scan.begin(starts::add));
        scan.complete(true, "next1234");
        assertEquals(2, starts.size());
    }
}
