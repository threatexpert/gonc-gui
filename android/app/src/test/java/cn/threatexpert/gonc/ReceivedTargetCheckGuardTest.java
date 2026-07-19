package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedTargetCheckGuardTest {
    @Test
    public void acceptsOnlyMatchingRunCheckPathAndSaveTree() {
        assertTrue(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 11, "docs", "docs", "content://tree/a", "content://tree/a",
                "Folder A", "Folder A", false));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 8, 11, 11, "docs", "docs", "content://tree/a", "content://tree/a",
                "Folder A", "Folder A", false));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 12, "docs", "docs", "content://tree/a", "content://tree/a",
                "Folder A", "Folder A", false));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 11, "docs", "other", "content://tree/a", "content://tree/a",
                "Folder A", "Folder A", false));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                7, 7, 11, 11, "docs", "docs", "content://tree/a", "content://tree/b",
                "Folder A", "Folder A", false));
    }

    @Test
    public void treatsTwoLegacyDownloadDestinationsAsTheSameSnapshot() {
        assertTrue(ReceivedTargetCheckGuard.isCurrent(
                2, 2, 3, 3, "", "", null, null, "Downloads/Gonc", "Downloads/Gonc", false));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                2, 2, 3, 3, "", "", null, "content://tree/a",
                "Downloads/Gonc", "Folder A", false));
    }

    @Test
    public void rejectsChangedSaveLabelAndShutdownController() {
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                4, 4, 9, 9, "docs", "docs", "content://tree/a", "content://tree/a",
                "Folder A", "Renamed Folder", false));
        assertFalse(ReceivedTargetCheckGuard.isCurrent(
                4, 4, 9, 9, "docs", "docs", "content://tree/a", "content://tree/a",
                "Folder A", "Folder A", true));
    }
}
