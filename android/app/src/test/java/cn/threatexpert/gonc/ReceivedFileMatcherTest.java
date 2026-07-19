package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedFileMatcherTest {
    @Test
    public void requiresReadableRegularMatchingTarget() {
        assertTrue(ReceivedFileMatcher.isAvailable(true, true, false, 5, 5));
        assertTrue(ReceivedFileMatcher.isAvailable(true, true, false, 0, 0));
        assertFalse(ReceivedFileMatcher.isAvailable(false, true, false, 0, 0));
        assertFalse(ReceivedFileMatcher.isAvailable(true, false, false, 5, 5));
        assertFalse(ReceivedFileMatcher.isAvailable(true, true, true, 5, 5));
        assertFalse(ReceivedFileMatcher.isAvailable(true, true, false, 4, 5));
    }

    @Test
    public void rejectsUnknownSizeButAcceptsExplicitZero() {
        assertEquals(-1, ReceivedFileMatcher.reportedSize(false, 0));
        assertEquals(-1, ReceivedFileMatcher.reportedSize(false, 99));
        assertEquals(0, ReceivedFileMatcher.reportedSize(true, 0));
        assertFalse(ReceivedFileMatcher.isAvailable(true, true, false,
                ReceivedFileMatcher.reportedSize(false, 0), 0));
    }

    @Test
    public void selectsReadableExactSizeInsteadOfLargestCollisionCandidate() {
        boolean[] readable = {true, true};
        boolean[] directory = {false, false};
        long[] sizes = {5, 8};

        assertEquals(0, ReceivedFileMatcher.firstAvailableIndex(readable, directory, sizes, 5));

        boolean[] fallbackReadable = {true, false, true};
        boolean[] fallbackDirectory = {false, false, false};
        long[] fallbackSizes = {8, 5, 5};
        assertEquals(2, ReceivedFileMatcher.firstAvailableIndex(
                fallbackReadable, fallbackDirectory, fallbackSizes, 5));
    }
}
