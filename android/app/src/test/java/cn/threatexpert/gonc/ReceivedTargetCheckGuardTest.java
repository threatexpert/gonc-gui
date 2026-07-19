package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedTargetCheckGuardTest {
    @Test
    public void acceptsOnlyMatchingRunCheckPathAndSaveTree() {
        assertTrue(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 11, "docs", "docs", "content://tree/a", "content://tree/a"));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 8, 11, 11, "docs", "docs", "content://tree/a", "content://tree/a"));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 12, "docs", "docs", "content://tree/a", "content://tree/a"));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 11, "docs", "other", "content://tree/a", "content://tree/a"));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 11, "docs", "docs", "content://tree/a", "content://tree/b"));
    }

    @Test
    public void treatsTwoLegacyDownloadDestinationsAsTheSameSnapshot() {
        assertTrue(ReceivedTargetCheckGuard.isCurrent(
                2, 2, 3, 3, "", "", null, null));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                2, 2, 3, 3, "", "", null, "content://tree/a"));
    }
}
