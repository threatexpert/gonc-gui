package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;

public class MainActivityUpdateTest {
    @Test
    public void canonicalUpdateVersionRemovesOnlyOneDebugSuffix() {
        assertEquals("1.2.17", MainActivity.canonicalUpdateVersion("1.2.17-debug"));
        assertEquals("1.2.17", MainActivity.canonicalUpdateVersion("1.2.17"));
        assertEquals("1.2.17-beta", MainActivity.canonicalUpdateVersion("1.2.17-beta"));
        assertEquals("1.2.17-debug",
                MainActivity.canonicalUpdateVersion("1.2.17-debug-debug"));
    }

    @Test
    public void failureKindsMapToLocalizedMessages() {
        assertEquals(R.string.update_network_error,
                MainActivity.updateFailureMessage(AndroidUpdateChecker.FailureKind.NETWORK));
        assertEquals(R.string.update_manifest_error,
                MainActivity.updateFailureMessage(AndroidUpdateChecker.FailureKind.INVALID_MANIFEST));
        assertEquals(R.string.update_platform_error,
                MainActivity.updateFailureMessage(AndroidUpdateChecker.FailureKind.UNSUPPORTED_PLATFORM));
    }
}
