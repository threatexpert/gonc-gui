package cn.threatexpert.gonc;

import org.junit.Test;

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
}
